import { lstat, open, readFile, realpath, rename, unlink, writeFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { confluenceMarkdown, planConfluence } from "../../url-to-markdown/scripts/confluence";

type SourceRepair = { path: string; pageId: string; version: number; changed: boolean; backup: string | null };
type FailureList = { root: string; unresolved: { path: string }[] };

/** 读取失败清单，限制在明确根目录内的 Markdown 文件，拒绝重复和越界路径。 */
function readFailures(text: string): FailureList {
  const value: object = JSON.parse(text);
  if (!value || typeof value !== "object" || !("root" in value) || typeof value.root !== "string" || !path.isAbsolute(value.root)
    || !("unresolved" in value) || !Array.isArray(value.unresolved)) throw new Error("失败清单须包含绝对路径 root 与 unresolved 数组");
  const seen = new Set<string>();
  const unresolved = value.unresolved.map((item: object) => {
    if (!item || typeof item !== "object" || !("path" in item) || typeof item.path !== "string"
      || path.posix.normalize(item.path) !== item.path || item.path.startsWith("../") || path.isAbsolute(item.path)
      || item.path.includes("\\") || !item.path.endsWith(".md") || seen.has(item.path)) throw new Error("失败清单含非法或重复 Markdown 路径");
    seen.add(item.path);
    return { path: item.path };
  });
  return { root: value.root, unresolved };
}

/** 有限重试同版本正文读取，不下载附件、不调用写接口。 */
async function sourcePage(base: string, cookie: string, pageId: string, version: number): Promise<string> {
  const url = `${base.replace(/\/$/, "")}/rest/api/content/${pageId}?version=${version}&expand=body.view,version`;
  for (let attempt = 1; ; attempt++) {
    try {
      const response = await fetch(url, { headers: { Cookie: cookie }, redirect: "error", signal: AbortSignal.timeout(60000) });
      if (!response.ok) throw new Error(`源页读取失败 HTTP ${response.status} url=${url}`);
      return JSON.stringify({ success: true, data: await response.json() });
    } catch (error) {
      if (attempt === 3) throw error;
      console.warn(JSON.stringify({ event: "source_read_retry", pageId, version, attempt }));
      await Bun.sleep(attempt * 250);
    }
  }
}

/** 校验源身份和版本后恢复正文，保留完整元数据与已有附件，先备份再原子写入。 */
export async function repairSourceFile(filename: string, base: string, cookie: string): Promise<SourceRepair> {
  if ((await realpath(filename)) !== filename || !(await lstat(filename)).isFile()) throw new Error("拒绝非普通文件或符号链接");
  const original = await readFile(filename);
  const markdown = original.toString("utf8");
  const header = markdown.match(/^(?:\uFEFF)?---\r?\n[\s\S]*?\r?\n---\r?\n/)?.[0];
  if (!header) throw new Error("Markdown 缺少完整元数据，不能按源版本恢复");
  const pageId = header.match(/^confluence_page_id: "(\d+)"\r?$/m)?.[1];
  const version = Number(header.match(/^confluence_version: (\d+)\r?$/m)?.[1]);
  const rawURL = header.match(/^url: (".*")\r?$/m)?.[1];
  if (!pageId || !Number.isSafeInteger(version) || version < 1 || !rawURL) throw new Error("缺少合法页面 ID、版本或 URL");
  const url: string = JSON.parse(rawURL);
  if (typeof url !== "string") throw new Error("页面 URL 必须为字符串");
  const address = new URL(url), instance = new URL(base);
  if (!["https:", "http:"].includes(instance.protocol) || instance.username || instance.password
    || instance.search || instance.hash || address.origin !== instance.origin || address.username || address.password
    || !address.pathname.startsWith(instance.pathname.replace(/\/$/, "") + "/")) throw new Error("源页 URL 与配置实例不一致，拒绝发送凭据");
  const page = await sourcePage(base, cookie, pageId, version);
  const plan = planConfluence(page, url);
  if (plan.pageId !== pageId || plan.version !== version) throw new Error(`源页版本不匹配 page_id=${pageId} expected=${version} actual=${plan.version}`);
  const assetNames: string[] = [];
  for (const item of plan.resources) {
    const asset = path.join(path.dirname(filename), "assets", item.localName);
    try {
      if (!(await lstat(asset)).isFile() || await realpath(asset) !== asset) throw new Error(`附件不是普通本地文件：${item.localName}`);
      assetNames.push(item.localName);
    } catch (error) {
      if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") throw error;
      // 原导出中未下载的附件继续保留源链接，不伪造本地文件。
    }
  }
  const body = confluenceMarkdown(page, url, assetNames);
  if (!body.trim()) throw new Error("源版本转换后正文为空，拒绝覆盖非空历史文件");
  const updated = Buffer.from(`${header}\n${body}\n`);
  const validation = Bun.spawn(["python3", "-c",
    "import sys;sys.path.insert(0,sys.argv[1]);from repair_tables import repair_markdown;s=sys.stdin.read();assert repair_markdown(s)==(s,0), 'source conversion is not stable'",
    path.dirname(fileURLToPath(import.meta.url))], { stdin: updated, stdout: "pipe", stderr: "pipe" });
  if (await validation.exited !== 0) throw new Error(`源版本转换未通过离线复核：${await new Response(validation.stderr).text()}`);
  const result: SourceRepair = { path: filename, pageId, version, changed: !original.equals(updated), backup: null };
  if (!result.changed) return result;
  if (!(await readFile(filename)).equals(original)) throw new Error("读取后文件已变化，拒绝覆盖");
  const backup = filename + ".bak";
  const mode = (await lstat(filename)).mode & 0o777;
  try {
    const stream = await open(backup, "wx", mode);
    try { await stream.writeFile(original); } finally { await stream.close(); }
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "EEXIST") throw error;
    if (!(await lstat(backup)).isFile() || await realpath(backup) !== backup || !(await readFile(backup)).equals(original)) {
      throw new Error(`备份冲突，拒绝覆盖：${backup}`);
    }
  }
  const temporary = filename + ".source-" + randomUUID();
  try {
    await writeFile(temporary, updated, { flag: "wx", mode });
    await rename(temporary, filename);
  } finally {
    await unlink(temporary).catch((error: NodeJS.ErrnoException) => { if (error.code !== "ENOENT") throw error; });
  }
  return { ...result, backup };
}

/** 显式源版本修复命令：每批十页，失败逐项报告，不上传或导入。 */
async function main(): Promise<void> {
  const args = process.argv.slice(2);
  if (args.length !== 2) throw new Error("用法：bun --env-file=.env repair_from_source.ts 失败清单.json 新结果.json");
  const base = process.env.CONFLUENCE_BASE_URL, cookie = process.env.CONFLUENCE_COOKIE;
  if (!base || !cookie) throw new Error("须配置 CONFLUENCE_BASE_URL 与 CONFLUENCE_COOKIE");
  const input = readFailures(await readFile(args[0], "utf8"));
  const root = await realpath(input.root);
  const output = await open(args[1], "wx", 0o600);
  const repaired: SourceRepair[] = [];
  const unresolved: { path: string; error: string }[] = [];
  try {
    await output.writeFile(JSON.stringify({ root, repaired, unresolved }, null, 2));
    for (let index = 0; index < input.unresolved.length; index += 10) {
      const batch = await Promise.all(input.unresolved.slice(index, index + 10).map(async item => {
        try { return await repairSourceFile(path.join(root, item.path), base, cookie); }
        catch (error) { return { path: item.path, error: error instanceof Error ? error.message : String(error) }; }
      }));
      for (const item of batch) {
        if ("error" in item) unresolved.push(item);
        else repaired.push(item);
      }
      await output.truncate(0);
      await output.write(JSON.stringify({ root, repaired, unresolved }, null, 2), 0, "utf8");
      console.log(JSON.stringify({ event: "source_repair_progress", checked: index + batch.length, total: input.unresolved.length, repaired: repaired.length, failed: unresolved.length }));
    }
  } finally { await output.close(); }
  if (unresolved.length) process.exitCode = 1;
}

if (import.meta.main) main().catch((error: Error) => { console.error(JSON.stringify({ event: "source_repair_failed", error: error.message })); process.exitCode = 1; });

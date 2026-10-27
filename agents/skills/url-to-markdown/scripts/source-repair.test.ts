import { afterAll, expect, test } from "bun:test";
import { mkdtemp, mkdir, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { planConfluence } from "./confluence";
import { repairSourceFile } from "../../repair-markdown-tables/scripts/repair_from_source";

const root = await realpath(await mkdtemp(path.join(tmpdir(), "source-repair-")));
const html = '<table><thead><tr><th>说明</th></tr></thead><tbody><tr><td><ul><li>甲</li><li>乙</li></ul><pre><code>type X = A | B;\n  run();</code></pre><img src="/download/attachments/123/shot.png"></td></tr></tbody></table>';
const response = (id: string, version: number, base: string) => ({ id, title: "源页", version: { number: version }, _links: { base }, body: { view: { value: html } } });
const server = Bun.serve({ hostname: "127.0.0.1", port: 0, fetch(request): Response {
  const url = new URL(request.url);
  if (request.headers.get("cookie") !== "session=source-test") return new Response("auth required", { status: 401 });
  const id = url.pathname.split("/").at(-1)!;
  return Response.json(response(id, id === "124" ? 5 : 4, url.origin));
} });
const base = `http://127.0.0.1:${server.port}`;

afterAll(async () => { await server.stop(true); await rm(root, { recursive: true }); });

test("源版本修复使用真实 HTTP，保留元数据、附件字节和备份，复杂结构不丢失", async () => {
  const directory = path.join(root, "same-version");
  await mkdir(path.join(directory, "assets"), { recursive: true });
  const url = `${base}/pages/viewpage.action?pageId=123`;
  const metadata = `---\nurl: ${JSON.stringify(url)}\nconfluence_page_id: "123"\nconfluence_version: 4\ncaptured_at: "original"\n---\n`;
  const original = metadata + '\n| broken\n';
  const filename = path.join(directory, "page.md");
  await writeFile(filename, original);
  const asset = planConfluence(JSON.stringify({ success: true, data: response("123", 4, base) }), url).resources[0].localName;
  const image = Buffer.from([137, 80, 78, 71]);
  await writeFile(path.join(directory, "assets", asset), image);
  const result = await repairSourceFile(filename, base, "session=source-test");
  const updated = await readFile(filename, "utf8");
  expect(result.changed).toBe(true);
  expect(updated.startsWith(metadata)).toBe(true);
  expect(updated).toContain('<ul><li>甲</li><li>乙</li></ul>');
  expect(updated).toContain('type X = A | B;&#10;  run();');
  expect(updated).toContain(`./assets/${asset}`);
  expect(await readFile(filename + '.bak', "utf8")).toBe(original);
  expect(await readFile(path.join(directory, "assets", asset))).toEqual(image);
  expect((await repairSourceFile(filename, base, "session=source-test")).changed).toBe(false);
});

test("源接口返回不同版本时明确失败，不改原文或生成备份", async () => {
  const filename = path.join(root, "mismatch.md");
  const original = `---\nurl: "${base}/pages/viewpage.action?pageId=124"\nconfluence_page_id: "124"\nconfluence_version: 4\n---\n\n原文\n`;
  await writeFile(filename, original);
  await expect(repairSourceFile(filename, base, "session=source-test")).rejects.toThrow("源页版本不匹配");
  expect(await readFile(filename, "utf8")).toBe(original);
  expect(await Bun.file(filename + ".bak").exists()).toBe(false);
});

test("HTTP 读取失败保留具体状态码，不覆盖正文", async () => {
  const filename = path.join(root, "unauthorized.md");
  const original = `---\nurl: "${base}/pages/viewpage.action?pageId=123"\nconfluence_page_id: "123"\nconfluence_version: 4\n---\n\n原文\n`;
  await writeFile(filename, original);
  await expect(repairSourceFile(filename, base, "invalid")).rejects.toThrow("HTTP 401");
  expect(await readFile(filename, "utf8")).toBe(original);
  expect(await Bun.file(filename + ".bak").exists()).toBe(false);
});

test("批量源恢复命令保存完整成功与失败报告，失败时返回非零", async () => {
  const directory = path.join(root, "batch");
  await mkdir(directory);
  for (const id of ['123', '124']) {
    await writeFile(path.join(directory, id + '.md'), `---\nurl: "${base}/pages/viewpage.action?pageId=${id}"\nconfluence_page_id: "${id}"\nconfluence_version: 4\n---\n\n原文\n`);
  }
  const input = path.join(root, "batch-input.json"), output = path.join(root, "batch-output.json");
  await writeFile(input, JSON.stringify({ root: directory, unresolved: [{ path: '123.md' }, { path: '124.md' }] }));
  const command = path.resolve(import.meta.dir, '../../repair-markdown-tables/scripts/repair_from_source.ts');
  const child = Bun.spawn([process.execPath, command, input, output], { env: { ...process.env, CONFLUENCE_BASE_URL: base, CONFLUENCE_COOKIE: 'session=source-test' }, stdout: 'pipe', stderr: 'pipe' });
  expect(await child.exited).toBe(1);
  const report = JSON.parse(await readFile(output, 'utf8'));
  expect(report.repaired).toHaveLength(1);
  expect(report.unresolved).toHaveLength(1);
  expect(report.unresolved[0].path).toBe('124.md');
  expect(report.unresolved[0].error).toContain('源页版本不匹配');
});

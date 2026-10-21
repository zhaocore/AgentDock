import { createHash } from "node:crypto";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { parseArgs } from "node:util";
import { parseHTML } from "linkedom";
import { httpUrl, resources, titleName, toMarkdown } from "./main";

type Page = { id: string; title: string; base: string; html: string; version: number };
type Attachment = { filename: string; size: number; encoding: string; content: string };
type ResourceRequest = { url: string; contentId: string; filename: string; localName: string };

/** 解析 MCP 文本块内的服务响应，失败响应不可作为成功数据。 */
function responseData(text: string): object {
  const response: object = JSON.parse(text);
  if (!response || typeof response !== "object" || !("success" in response) || response.success !== true
    || !("data" in response) || !response.data || typeof response.data !== "object") {
    throw new Error("MCP 响应必须为成功的 {success:true,data:{...}} JSON 文本块");
  }
  return response.data;
}

/** 要求渲染 HTML，避免直接转换 storage 丢失图片与宏内容。 */
function readPage(text: string): Page {
  const page = responseData(text);
  if (!("id" in page) || typeof page.id !== "string" || !/^\d+$/.test(page.id)
    || !("title" in page) || typeof page.title !== "string"
    || !("body" in page) || !page.body || typeof page.body !== "object"
    || !("view" in page.body) || !page.body.view || typeof page.body.view !== "object"
    || !("value" in page.body.view) || typeof page.body.view.value !== "string" || !page.body.view.value.trim()
    || !("_links" in page) || !page._links || typeof page._links !== "object"
    || !("base" in page._links) || typeof page._links.base !== "string"
    || !("version" in page) || !page.version || typeof page.version !== "object"
    || !("number" in page.version) || !Number.isSafeInteger(page.version.number)) {
    throw new Error("页面 MCP 响应缺少 id、title、body.view.value、_links.base 或 version.number；请 expand body.view,version");
  }
  return { id: page.id, title: page.title, html: page.body.view.value,
    base: httpUrl(page._links.base).href, version: page.version.number as number };
}

/** 保留正文，删除目录宏生成的界面和脚本样式。 */
function pageDocument(page: Page): Document {
  const { document } = parseHTML(`<html><body>${page.html}</body></html>`);
  for (const node of document.querySelectorAll("script,style,noscript,template,colgroup,[data-macro-name=easy-heading-free]")) node.remove();
  for (const image of document.querySelectorAll("img[data-image-src]")) {
    image.setAttribute("src", image.getAttribute("data-image-src")!);
  }
  for (const link of document.querySelectorAll("a[href]")) {
    if (/\/download\/attachments\//.test(new URL(link.getAttribute("href")!, page.base).pathname)) link.setAttribute("download", "");
  }
  for (const macro of document.querySelectorAll('[data-macro-name="jira"]')) {
    const link = macro.querySelector("a.jira-issue-key");
    if (link && macro.querySelector(".summary")?.textContent?.trim() === "Getting issue details...") {
      const copy = link.cloneNode(true) as Element;
      for (const placeholder of copy.querySelectorAll(".issue-placeholder")) placeholder.remove();
      macro.replaceWith(copy);
    }
  }
  return document;
}

/** 从正文定位附件所属页面，拒绝把其他实例交给当前 MCP 下载。 */
function resourceRequest(url: string, base: string): ResourceRequest {
  const source = httpUrl(url);
  const match = source.pathname.match(/\/download\/(?:attachments|thumbnails)\/(\d+)\/([^/]+)$/);
  if (source.origin !== new URL(base).origin || !match) {
    throw new Error(`资源不是当前 Confluence 实例的附件，需另行下载并核对：${source.origin}${source.pathname}`);
  }
  const contentId = match[1];
  const filename = decodeURIComponent(match[2]);
  const extension = path.extname(filename).toLowerCase();
  const hash = createHash("sha256").update(`${contentId}:${filename}`).digest("hex").slice(0, 24);
  return { url, contentId, filename, localName: `asset-${hash}${/^\.[a-z0-9]{1,8}$/.test(extension) ? extension : ".bin"}` };
}

/** 生成 MCP 下载计划，附件按页面与文件名去重，不依赖分页列表。 */
export function planConfluence(pageResponse: string, url: string): { pageId: string; title: string; version: number; resources: ResourceRequest[] } {
  const page = readPage(pageResponse);
  const source = httpUrl(url);
  const pageId = source.searchParams.get("pageId") || source.pathname.match(/\/pages\/(\d+)(?:\/|$)/)?.[1];
  if (pageId !== page.id || source.origin !== new URL(page.base).origin) {
    throw new Error("输入 URL 的页面 ID 或实例与 MCP 响应不匹配；请先解析当前实例的页面 URL");
  }
  const refs = resources(pageDocument(page), page.base);
  const unique = new Map<string, ResourceRequest>();
  for (const ref of refs) {
    const request = resourceRequest(ref.url, page.base);
    unique.set(request.localName, request);
  }
  return { pageId: page.id, title: page.title, version: page.version, resources: [...unique.values()] };
}

/** 校验附件响应身份、内容编码与声明字节数，防止错图和被截断的数据。 */
function attachmentBytes(data: object, request: ResourceRequest): Buffer {
  if (!("contentId" in data) || data.contentId !== request.contentId
    || !("attachments" in data) || !Array.isArray(data.attachments)) {
    throw new Error(`附件 MCP 响应的 contentId 或 attachments 无效：${request.contentId}`);
  }
  const candidates: object[] = data.attachments.filter((item: object) => item && typeof item === "object"
    && "filename" in item && item.filename === request.filename);
  if (candidates.length !== 1) throw new Error(`附件响应未精确命中一个文件：${request.contentId}/${request.filename}；不可使用其他图片替代`);
  const item = candidates[0];
  if (!("size" in item) || !Number.isSafeInteger(item.size) || (item.size as number) < 0
    || !("encoding" in item) || item.encoding !== "base64"
    || !("content" in item) || typeof item.content !== "string") {
    throw new Error(`附件缺少合法的 base64 内容：${request.filename}；检查 returnContent 与 maxInlineBytes`);
  }
  const attachment = item as Attachment;
  const bytes = Buffer.from(attachment.content, "base64");
  if (bytes.toString("base64") !== attachment.content) throw new Error(`附件 base64 数据无效：${request.filename}`);
  if (bytes.length !== attachment.size) throw new Error(`附件字节数不符：${request.filename}，期望 ${attachment.size}，实际 ${bytes.length}`);
  return bytes;
}

/** 将宿主 MCP 结果导出为标题目录、同名 Markdown 与 assets，不需要浏览器或 Cookie。 */
export async function exportConfluence(pageResponse: string, url: string, attachmentsDir: string, outputDir: string): Promise<string> {
  const page = readPage(pageResponse);
  const plan = planConfluence(pageResponse, url);
  const document = pageDocument(page);
  const refs = resources(document, page.base);
  const responses = new Map<string, object>();
  const files = new Map<string, Buffer>();
  for (const request of plan.resources) {
    let response = responses.get(request.contentId);
    if (!response) {
      response = responseData(await readFile(path.join(attachmentsDir, `${request.contentId}.json`), "utf8"));
      responses.set(request.contentId, response);
    }
    files.set(request.localName, attachmentBytes(response, request));
  }
  for (const ref of refs) ref.node.setAttribute(ref.attribute, `./assets/${resourceRequest(ref.url, page.base).localName}`);
  const markdown = toMarkdown(document.body.innerHTML);
  if (!markdown.trim()) throw new Error("Confluence 转换后正文为空");
  const name = titleName(page.title);
  const destination = path.resolve(outputDir, name);
  await mkdir(path.dirname(destination), { recursive: true });
  await mkdir(destination);
  try {
    await mkdir(path.join(destination, "assets"));
    for (const [filename, bytes] of files) await writeFile(path.join(destination, "assets", filename), bytes, { flag: "wx" });
    const metadata = ["---", `url: ${JSON.stringify(url)}`, `title: ${JSON.stringify(page.title)}`,
      `confluence_page_id: ${JSON.stringify(page.id)}`, `confluence_version: ${page.version}`,
      `captured_at: ${JSON.stringify(new Date().toISOString())}`, "---", "", markdown, ""].join("\n");
    const output = path.join(destination, `${name}.md`);
    await writeFile(output, metadata, { flag: "wx" });
    return output;
  } catch (error) {
    await rm(destination, { recursive: true });
    throw error;
  }
}

/** CLI 仅消费已由宿主读取的 MCP JSON，不持有远程鉴权信息。 */
async function main(): Promise<void> {
  const args = parseArgs({ allowPositionals: true, options: {
    url: { type: "string" }, "attachments-dir": { type: "string" }, "output-dir": { type: "string" },
  } });
  if (args.positionals.length !== 2 || !["plan", "export"].includes(args.positionals[0]) || !args.values.url) {
    throw new Error("用法：bun confluence.ts plan|export page.json --url URL [--attachments-dir DIR --output-dir EXPORT_ROOT]");
  }
  const page = await readFile(args.positionals[1], "utf8");
  if (args.positionals[0] === "plan") {
    console.log(JSON.stringify(planConfluence(page, args.values.url), null, 2));
    return;
  }
  if (!args.values["attachments-dir"] || !args.values["output-dir"]) throw new Error("export 必须提供 --attachments-dir 和 --output-dir");
  console.log(await exportConfluence(page, args.values.url, args.values["attachments-dir"], args.values["output-dir"]));
}

if (import.meta.main) {
  main().catch((error: Error) => {
    console.error(JSON.stringify({ event: "confluence_export_failed", message: error.message }));
    process.exitCode = 1;
  });
}

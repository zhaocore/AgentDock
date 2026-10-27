import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { parseArgs } from "node:util";
import { fileURLToPath } from "node:url";
import { parseHTML } from "linkedom";
import { chromium, type BrowserContext } from "playwright";
import TurndownService from "turndown";
import { gfm } from "turndown-plugin-gfm";

type CookieRule = { domain: string; cookie: string };
type Options = { url: string; config: string | null; outputDir: string | null; timeout: number };
type Resource = { node: Element; attribute: string; url: string };
const skillDir = fileURLToPath(new URL("../", import.meta.url));
const attachments = /\.(pdf|docx?|xlsx?|pptx?|csv|tsv|zip|gz|tar|rar|7z|txt|md|epub|png|jpe?g|gif|webp|svg|avif|ico|mp[34]|wav|ogg|webm|mov|vtt|srt)$/i;

/** 标题用于目录与文件名，替换非法字符且禁止空名称与路径穿越。 */
export function titleName(title: string): string {
  const name = title.replace(/[<>:"/\\|?*\x00-\x1f\x7f]/g, "-").trim().replace(/[. ]+$/g, "");
  if (!name) throw new Error("页面标题为空或不能形成安全文件名，请先确认页面标题");
  return /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(name) ? `_${name}` : name;
}

/** 校验 URL 协议，避免将本地文件作为远程资源读取。 */
export function httpUrl(value: string): URL {
  const url = new URL(value);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) {
    throw new Error("URL 必须使用 HTTP/HTTPS，且不得包含用户名或密码");
  }
  return url;
}

/** 验证外部配置，不在错误消息中包含 Cookie 内容。 */
export function parseCookies(text: string): CookieRule[] {
  let parsed: object;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw new Error("Cookie 配置不是合法 JSON，请检查语法");
  }
  if (!parsed || typeof parsed !== "object" || !("cookies" in parsed) || !Array.isArray(parsed.cookies)) {
    throw new Error("Cookie 配置必须包含 cookies 数组");
  }
  return parsed.cookies.map((entry: object, index: number): CookieRule => {
    if (!entry || typeof entry !== "object" || !("domain" in entry) || typeof entry.domain !== "string"
      || !("cookie" in entry) || typeof entry.cookie !== "string") {
      throw new Error(`cookies[${index}] 必须包含 domain 和 cookie 字符串`);
    }
    const domain = entry.domain.toLowerCase();
    if (!/^\.?[a-z0-9]+(?:[a-z0-9.-]*[a-z0-9])?$/.test(domain) || domain.includes("..")) {
      throw new Error(`cookies[${index}].domain 必须为主机名，可带一个点前缀`);
    }
    if (!entry.cookie || /[\x00-\x1f\x7f]/.test(entry.cookie) || entry.cookie.split(";").some((pair: string) => {
      const separator = pair.indexOf("=");
      return separator < 1 || !/^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/.test(pair.slice(0, separator).trim());
    })) {
      throw new Error(`cookies[${index}].cookie 必须为 name=value; name=value 请求头字符串`);
    }
    return { domain, cookie: entry.cookie };
  });
}

/** 对外部请求有限重试，最终保留具体异常。 */
async function retry<T>(operation: () => Promise<T>, label: string): Promise<T> {
  for (let attempt = 1; ; attempt++) {
    try {
      return await operation();
    } catch (error) {
      if (attempt === 3) throw error;
      console.warn(JSON.stringify({ event: "request_retry", operation: label, attempt }));
      await Bun.sleep(attempt * 250);
    }
  }
}

/** 通过浏览器 Cookie jar 下载，重定向由请求上下文按域隔离。 */
async function download(context: BrowserContext, url: string, referer: string, timeout: number): Promise<{ body: Buffer; extension: string }> {
  if (url.startsWith("data:")) {
    const response = await fetch(url);
    const mime = response.headers.get("content-type")?.split(";")[0] || "";
    return { body: Buffer.from(await response.arrayBuffer()), extension: mimeExtension(mime) || ".bin" };
  }
  httpUrl(url);
  return retry(async () => {
    const response = await context.request.get(url, { timeout, headers: { referer } }).catch((error: Error) => {
      // Playwright 调用日志可能含请求头，只保留首行异常原因。
      throw new Error(error.message.split("\n")[0]);
    });
    try {
      const address = new URL(url);
      if (!response.ok()) throw new Error(`资源下载失败，HTTP ${response.status()}，地址 ${address.origin}${address.pathname}`);
      const mime = (response.headers()["content-type"] || "").split(";")[0].trim();
      if (["text/html", "application/xhtml+xml"].includes(mime)) {
        throw new Error(`资源返回 HTML，可能需要登录，HTTP ${response.status()}，主机 ${new URL(url).hostname}`);
      }
      const extension = path.extname(new URL(url).pathname).toLowerCase();
      return { body: await response.body(), extension: mimeExtension(mime) || (/^\.[a-z0-9]{1,8}$/.test(extension) ? extension : ".bin") };
    } finally {
      await response.dispose();
    }
  }, "download_resource");
}

/** 使用响应类型确定无扩展名资源的本地后缀。 */
function mimeExtension(mime: string): string {
  const extensions: Record<string, string> = {
    "image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
    "image/svg+xml": ".svg", "image/avif": ".avif", "application/pdf": ".pdf",
    "video/mp4": ".mp4", "video/webm": ".webm", "audio/mpeg": ".mp3", "audio/ogg": ".ogg",
    "audio/wav": ".wav", "text/vtt": ".vtt",
  };
  return extensions[mime] || "";
}

/** 提取正文资源，普通链接转为绝对地址，锚点和不可解析引用保留原值。 */
export function resources(document: Document, base: string): Resource[] {
  const result: Resource[] = [];
  for (const node of document.querySelectorAll("img,video,audio,source,track,object,embed,a")) {
    const attribute = node.tagName === "A" ? "href" : node.tagName === "OBJECT" ? "data" : "src";
    const raw = node.getAttribute(attribute);
    const url = raw && !raw.startsWith("#") ? URL.parse(raw, base) : null;
    if (url) {
      const isResource = node.tagName !== "A" || node.hasAttribute("download") || attachments.test(url.pathname);
      if (isResource) {
        url.hash = "";
        result.push({ node, attribute, url: url.href });
      } else if (["http:", "https:", "mailto:", "tel:"].includes(url.protocol)) {
        node.setAttribute(attribute, url.href);
      } else {
        node.removeAttribute(attribute);
      }
    }
    const poster = node.getAttribute("poster");
    const posterUrl = poster ? URL.parse(poster, base) : null;
    if (posterUrl) result.push({ node, attribute: "poster", url: posterUrl.href });
  }
  return result;
}

/** 复杂单元格无法用 GFM 行内语法无损表示，保留完整 HTML 表格。 */
function needsHtmlTable(table: Element): boolean {
  return [...table.querySelectorAll("table,pre,ul,ol,blockquote,h1,h2,h3,h4,h5,h6")].some(node => node !== table)
    || table.querySelectorAll("thead tr").length > 1;
}

/** 展开简单表格合并单元格，跨行内容重复保留，跨列覆盖位置留空。 */
function expandTableSpans(html: string): string {
  const { document } = parseHTML(`<html><body>${html}</body></html>`);
  for (const node of document.querySelectorAll("colgroup")) node.remove();
  for (const table of [...document.querySelectorAll("table")].reverse()) {
    if (needsHtmlTable(table)) continue;
    const rows = [...table.querySelectorAll("tr")].filter(row => row.closest("table") === table);
    const grid: (Element | null)[][] = rows.map(() => []);
    rows.forEach((row, rowIndex) => {
      let column = 0;
      for (const cell of [...row.children].filter(node => ["th", "td"].includes(node.localName))) {
        while (grid[rowIndex][column] !== undefined) column++;
        const height = Number(cell.getAttribute("rowspan") ?? "1");
        const width = Number(cell.getAttribute("colspan") ?? "1");
        if (!Number.isSafeInteger(height) || height < 0 || !Number.isSafeInteger(width) || width < 1 || width > 1000) {
          throw new Error("表格 rowspan/colspan 必须为合法整数，colspan 范围为 1 至 1000");
        }
        const remaining = rows.slice(rowIndex).filter(other => other.parentElement === row.parentElement).length;
        const span = height === 0 ? remaining : Math.min(height, remaining);
        for (let down = 0; down < span; down++) {
          for (let right = 0; right < width; right++) {
            if (grid[rowIndex + down][column + right] !== undefined) throw new Error("表格合并单元格位置重叠，无法展开");
            grid[rowIndex + down][column + right] = right === 0 ? cell : null;
          }
        }
        column += width;
      }
    });
    const columns = grid.reduce((maximum, row) => Math.max(maximum, row.length), 0);
    rows.forEach((row, rowIndex) => row.replaceChildren(...Array.from({ length: columns }, (_, column) => {
      const source = grid[rowIndex][column];
      const cell = source ? source.cloneNode(true) as Element : document.createElement(row.parentElement?.localName === "thead" ? "th" : "td");
      cell.removeAttribute("rowspan");
      cell.removeAttribute("colspan");
      return cell;
    })));
  }
  return document.body.innerHTML;
}

/** 保留表格行列关系及媒体原位链接，避免转换器丢失合并信息或下载引用。 */
export function toMarkdown(html: string): string {
  const converter = new TurndownService({ headingStyle: "atx", codeBlockStyle: "fenced", bulletListMarker: "-" });
  converter.use(gfm);
  converter.addRule("structuredTable", {
    filter: node => node.nodeName === "TABLE" && needsHtmlTable(node as Element),
    replacement: (_content, node) => `\n\n${(node as Element).outerHTML.replace(/\r?\n/g, "&#10;")}\n\n`,
  });
  // GFM 每行必须完整，段落和换行不能将单元格拆成多行。
  converter.addRule("tableCell", {
    filter: ["th", "td"],
    replacement: (content, node) => {
      const cell = content.trim().replace(/\s*\n+\s*/g, "<br>").replace(/\|/g, "\\|");
      return `${node.previousElementSibling ? " " : "| "}${cell} |`;
    },
  });
  converter.addRule("media", {
    filter: ["video", "audio", "source", "track", "object", "embed"],
    replacement: (content, node) => {
      const media = node as HTMLElement;
      const src = media.getAttribute("src") || media.getAttribute("data");
      const poster = media.getAttribute("poster");
      return `\n\n${poster ? `![poster](${poster})\n\n` : ""}${src ? `[${media.tagName.toLowerCase()}](${src})` : ""}${content}\n\n`;
    },
  });
  return converter.turndown(expandTableSpans(html));
}

/** 浏览器渲染后导出标题目录、同名 Markdown 与 assets；失败清理本次目录。 */
export async function capture(options: Options): Promise<string> {
  const input = httpUrl(options.url);
  const rules = options.config === null ? [] : parseCookies(await readFile(options.config, "utf8"));
  const browser = await chromium.launch({
    channel: process.env.URL_BROWSER_CHANNEL || "chrome",
    ...(process.env.URL_CHROME_PATH ? { executablePath: process.env.URL_CHROME_PATH } : {}),
    headless: true,
  });
  let output: string | null = null;
  try {
    const context = await browser.newContext({ acceptDownloads: false });
    await context.addCookies(rules.flatMap(rule => rule.cookie.split(";").map(pair => {
      const separator = pair.indexOf("=");
      return {
        name: pair.slice(0, separator).trim(), value: pair.slice(separator + 1).trim(), domain: rule.domain,
        path: "/", secure: !(input.protocol === "http:" && rule.domain === input.hostname),
      };
    })));
    const page = await context.newPage();
    page.setDefaultTimeout(options.timeout);
    await retry(async () => {
      const response = await page.goto(input.href, { waitUntil: "load", timeout: options.timeout });
      if (!response || !response.ok()) throw new Error(`页面加载失败，HTTP ${response?.status() ?? "无响应"}，主机 ${input.hostname}`);
    }, "navigate_page");
    await page.waitForLoadState("networkidle", { timeout: options.timeout });
    await page.evaluate(async () => {
      for (let step = 0; step < 30; step++) {
        window.scrollBy(0, window.innerHeight);
        await new Promise<void>(resolve => setTimeout(resolve, 100));
        if (window.scrollY + window.innerHeight >= document.documentElement.scrollHeight) break;
      }
    });
    await page.waitForLoadState("networkidle", { timeout: options.timeout });
    const extracted = await page.evaluate(() => {
      const root = document.querySelector("article, main, [role=main]") || document.body;
      const clone = root.cloneNode(true) as HTMLElement;
      const originals = root.querySelectorAll("img");
      clone.querySelectorAll("img").forEach((image, index) => {
        image.setAttribute("src", originals[index].currentSrc || image.getAttribute("data-src") || image.src);
        image.removeAttribute("srcset");
      });
      clone.querySelectorAll("script,style,noscript,template,iframe,picture source").forEach(node => node.remove());
      return { title: document.title, base: document.baseURI, url: location.href, html: clone.innerHTML };
    });
    httpUrl(extracted.url);
    if (!extracted.html.trim()) throw new Error("页面正文为空，无法导出");
    const { document } = parseHTML(`<html><body>${extracted.html}</body></html>`);
    const refs = resources(document, extracted.base);
    const name = titleName(extracted.title);
    const parent = options.outputDir ? path.resolve(options.outputDir) : path.join(skillDir, "output", input.hostname);
    await mkdir(parent, { recursive: true });
    const directory = path.join(parent, name);
    await mkdir(directory);
    output = directory;
    await mkdir(path.join(output, "assets"));
    const saved = new Map<string, string>();
    for (const ref of refs) {
      let filename = saved.get(ref.url);
      if (!filename) {
        const resource = await download(context, ref.url, extracted.url, options.timeout);
        filename = `asset-${createHash("sha256").update(ref.url).digest("hex").slice(0, 24)}${resource.extension}`;
        await writeFile(path.join(output, "assets", filename), resource.body, { flag: "wx" });
        saved.set(ref.url, filename);
      }
      ref.node.setAttribute(ref.attribute, `./assets/${filename}`);
    }
    const markdown = toMarkdown(document.body.innerHTML);
    if (!markdown.trim()) throw new Error("转换后 Markdown 为空，无法导出");
    const metadata = ["---", `url: ${JSON.stringify(extracted.url)}`, `title: ${JSON.stringify(extracted.title)}`,
      `captured_at: ${JSON.stringify(new Date().toISOString())}`, "---", "", markdown, ""].join("\n");
    const destination = path.join(output, `${name}.md`);
    await writeFile(destination, metadata, { flag: "wx" });
    return destination;
  } catch (error) {
    if (output !== null) await rm(output, { recursive: true });
    throw error;
  } finally {
    await browser.close();
  }
}

/** CLI 参数只接收配置路径，不在命令行传递 Cookie。 */
async function main(): Promise<void> {
  const args = parseArgs({ allowPositionals: true, options: {
    config: { type: "string" }, "output-dir": { type: "string" }, timeout: { type: "string" }, help: { type: "boolean" },
  } });
  if (args.values.help) {
    console.log("bun scripts/main.ts <url> [--config file.json] [--output-dir export-root] [--timeout ms]");
    return;
  }
  if (args.positionals.length !== 1) throw new Error("必须提供一个 URL；使用 --help 查看用法");
  const timeout = Number(args.values.timeout ?? "30000");
  if (!Number.isSafeInteger(timeout) || timeout < 1) throw new Error("timeout 必须为正整数毫秒数");
  const localConfig = path.join(skillDir, "cookies.local.json");
  const config = args.values.config ?? (existsSync(localConfig) ? localConfig : null);
  console.log(await capture({ url: args.positionals[0], config, outputDir: args.values["output-dir"] ?? null, timeout }));
}

if (import.meta.main) {
  main().catch((error: Error) => {
    console.error(JSON.stringify({ event: "capture_failed", message: error.message }));
    process.exitCode = 1;
  });
}

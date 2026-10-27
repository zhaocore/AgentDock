import { afterAll, expect, test } from "bun:test";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { chromium } from "playwright";
import { capture, httpUrl, parseCookies, titleName, toMarkdown } from "./main";

const root = await mkdtemp(path.join(tmpdir(), "url-markdown-test-"));
const config = path.join(root, "cookies.json");

test("rowspan=0 限于所在行组，合并表头展开，非法跨度明确失败", () => {
  const markdown = toMarkdown('<table><thead><tr><th colspan="2">标题</th></tr></thead><tbody><tr><td rowspan="0">组一</td><td>A</td></tr><tr><td>B</td></tr></tbody><tbody><tr><td>组二</td><td>C</td></tr></tbody></table>');
  expect(markdown).toContain('| 标题 |  |');
  expect(markdown).toContain('| 组一 | A |');
  expect(markdown).toContain('| 组一 | B |');
  expect(markdown).toContain('| 组二 | C |');
  expect(() => toMarkdown('<table><tr><th colspan="bad">标题</th></tr></table>')).toThrow('rowspan/colspan');
});

test("复杂表格完整保留结构，真实浏览器正常展示列表及代码缩进", async () => {
  const html = '<table data-testid="structured-table"><thead><tr><th>说明</th></tr></thead><tbody><tr><td rowspan="2"><h3>信息</h3><ul><li>A</li><li>B</li></ul><pre data-testid="source-code"><code>type X = A | B;\n  run();</code></pre></td></tr><tr></tr></tbody></table>';
  const markdown = toMarkdown(html);
  expect(markdown).toContain('<table data-testid="structured-table">');
  expect(markdown).toContain('<td rowspan="2">');
  expect(markdown).toContain('<ul><li>A</li><li>B</li></ul>');
  expect(markdown).toContain('type X = A | B;&#10;  run();');
  expect(markdown).not.toContain('```<br>');
  const browser = await chromium.launch({ channel: process.env.URL_BROWSER_CHANNEL || "chrome", headless: true,
    ...(process.env.URL_CHROME_PATH ? { executablePath: process.env.URL_CHROME_PATH } : {}) });
  try {
    const page = await browser.newPage();
    await page.setContent(markdown);
    expect(await page.locator('[data-testid="structured-table"]').isVisible()).toBe(true);
    expect(await page.locator('[data-testid="structured-table"] li').count()).toBe(2);
    expect(await page.locator('[data-testid="source-code"]').textContent()).toBe('type X = A | B;\n  run();');
  } finally { await browser.close(); }
});
await writeFile(config, JSON.stringify({ cookies: [{ domain: "127.0.0.1", cookie: "session=integration-secret; token=a=b" }] }));
const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jGZkAAAAASUVORK5CYII=", "base64");
let crossDomainCookie = "";
let pageAttempts = 0;
let missingAttempts = 0;
const server = Bun.serve({
  hostname: "0.0.0.0", port: 0,
  fetch(request): Response {
    const url = new URL(request.url);
    if (url.pathname === "/cross") {
      crossDomainCookie = request.headers.get("cookie") || "";
      return new Response(png, { headers: { "content-type": "image/png" } });
    }
    if (url.pathname === "/redirect") {
      return Response.redirect(`http://localhost:${server.port}/cross`);
    }
    if (url.pathname === "/missing.png") {
      missingAttempts++;
      return new Response("missing", { status: 404 });
    }
    if (!request.headers.get("cookie")?.includes("session=integration-secret")) {
      return new Response("auth required", { status: 401 });
    }
    if (url.pathname === "/page") {
      pageAttempts++;
      if (pageAttempts === 1) return new Response("retry", { status: 503 });
      return new Response(`<!doctype html><html><head><meta charset="utf-8"><title>集成测试</title><base href="/files/"></head><body>
        <nav>导航不应进入正文</nav><article><h1>正文</h1><p>图片之前</p>
        <picture><source srcset="/image.png"><img src="/image.png" alt="截图"></picture>
        <p>图片之后</p><img src="/image.png" alt="重复图片"><img src="/redirect" alt="跨域图">
        <a href="guide.pdf">附件</a><a href="../next">下一页</a><a href="#anchor">锚点</a>
        <a href="resCors://enable localhost:4200/infiniteStage/remoteEntry.js">自定义链接</a><img src="https://[bad" alt="原引用">
        <audio src="/sound.mp3"></audio><video poster="/image.png"><source src="/movie.mp4"></video>
        <table><thead><tr><th>A</th></tr></thead><tbody><tr><td>B</td></tr></tbody></table>
        <pre><code>const x = 1;</code></pre></article>
        <script>setTimeout(() => {const p=document.createElement('p');p.textContent='动态渲染正文';document.querySelector('article').append(p)},20)</script>
        </body></html>`, { headers: { "content-type": "text/html" } });
    }
    if (url.pathname === "/broken") {
      return new Response('<title>失败测试</title><article><h1>失败测试</h1><img src="/missing.png"></article>', { headers: { "content-type": "text/html; charset=utf-8" } });
    }
    if (url.pathname === "/image.png") return new Response(png, { headers: { "content-type": "image/png" } });
    if (url.pathname === "/files/guide.pdf") return new Response("%PDF-test", { headers: { "content-type": "application/pdf" } });
    if (url.pathname === "/sound.mp3") return new Response("audio-bytes", { headers: { "content-type": "audio/mpeg" } });
    if (url.pathname === "/movie.mp4") return new Response("video-bytes", { headers: { "content-type": "video/mp4" } });
    return new Response("not found", { status: 404 });
  },
});
const base = `http://127.0.0.1:${server.port}`;

afterAll(async () => {
  await server.stop(true);
  await rm(root, { recursive: true });
});

test("真实浏览器：Cookie 鉴权、动态正文、资源同目录及原位链接、跨域重定向隔离", async () => {
  const outputDir = path.join(root, "success");
  const result = await capture({ url: `${base}/page`, config, outputDir, timeout: 10000 });
  const markdown = await readFile(result, "utf8");
  const directory = path.join(outputDir, "集成测试");
  expect(result).toBe(path.join(directory, "集成测试.md"));
  expect((await readdir(directory)).sort()).toEqual(["assets", "集成测试.md"]);
  const files = await readdir(path.join(directory, "assets"));
  expect(files.length).toBe(5);
  expect(markdown).toContain("动态渲染正文");
  expect(markdown).not.toContain("导航不应进入正文");
  expect(markdown).not.toContain("integration-secret");
  expect(markdown).toContain(`[下一页](${base}/next)`);
  expect(markdown).toContain("[锚点](#anchor)");
  expect(markdown).toContain("[自定义链接](<resCors://enable localhost:4200/infiniteStage/remoteEntry.js>)");
  expect(markdown).toContain("![原引用](https://[bad)");
  expect(markdown).toContain("| A |");
  expect(markdown).toContain("```\nconst x = 1;\n```");
  expect(markdown.indexOf("图片之前")).toBeLessThan(markdown.indexOf("![截图]"));
  expect(markdown.indexOf("![截图]")).toBeLessThan(markdown.indexOf("图片之后"));
  const localLinks = [...markdown.matchAll(/\]\(\.\/assets\/([^\)]+)\)/g)].map(match => match[1]);
  expect(localLinks.length).toBe(7);
  for (const link of localLinks) expect(files).toContain(link);
  const image = files.find(file => file.endsWith(".png"));
  expect(await readFile(path.join(directory, "assets", image!))).toEqual(png);
  expect(crossDomainCookie).toBe("");
  expect(pageAttempts).toBe(2);
  await expect(capture({ url: `${base}/page`, config, outputDir, timeout: 10000 })).rejects.toThrow("EEXIST");
  expect(await readFile(result, "utf8")).toBe(markdown);
}, 30000);

test("资源失败重试后清理目录，缺失 Cookie 明确失败", async () => {
  const outputDir = path.join(root, "failure");
  await expect(capture({ url: `${base}/broken`, config, outputDir, timeout: 10000 })).rejects.toThrow("HTTP 404");
  expect(missingAttempts).toBeGreaterThanOrEqual(3);
  expect(await readdir(outputDir)).toEqual([]);
  await expect(capture({ url: `${base}/page`, config: null, outputDir, timeout: 10000 })).rejects.toThrow("HTTP 401");
}, 30000);

test("配置边界：合法 Cookie 值保留等号，拒绝错误配置与非 HTTP URL", () => {
  expect(parseCookies('{"cookies":[{"domain":".Example.com","cookie":"session=a=b"}]}'))
    .toEqual([{ domain: ".example.com", cookie: "session=a=b" }]);
  for (const value of ["null", "{}", '{"cookies":[null]}', '{"cookies":[{"domain":"https://x","cookie":"a=b"}]}',
    '{"cookies":[{"domain":"x","cookie":"bad"}]}', '{"cookies":[{"domain":"x","cookie":"a=b\\nc=d"}]}']) {
    expect(() => parseCookies(value)).toThrow();
  }
  expect(() => httpUrl("file:///etc/passwd")).toThrow();
  expect(() => httpUrl("https://user:pass@example.com")).toThrow();
  expect(() => parseCookies('{"cookies": "private-cookie-secret"')).toThrow("Cookie 配置不是合法 JSON");
});

test("标题保留中文和空格，非法路径字符与保留文件名安全转换", () => {
  expect(titleName("RN Plugin 常规发布\\Adhoc\\Hotfix流程")).toBe("RN Plugin 常规发布-Adhoc-Hotfix流程");
  expect(titleName("  文档 / 标题:测试?  ")).toBe("文档 - 标题-测试-");
  expect(titleName("CON")).toBe("_CON");
  expect(titleName("报告. ")).toBe("报告");
  expect(() => titleName(".. ")).toThrow("标题");
  expect(() => titleName(" ")).toThrow("标题");
});

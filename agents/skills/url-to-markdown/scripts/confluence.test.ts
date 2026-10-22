import { expect, test } from "bun:test";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { exportConfluence, planConfluence } from "./confluence";

const url = "https://confluence.example.com/pages/viewpage.action?pageId=123";
const page = JSON.stringify({ success: true, data: {
  id: "123", title: "示例页面", version: { number: 4 }, _links: { base: "https://confluence.example.com" },
  body: { view: { value: '<style>不得进入正文</style><div data-macro-name="easy-heading-free">目录界面</div><h1>开始</h1><p>图片之前</p><img src="/download/thumbnails/456/shot.png"><p>图片之后</p><img src="/download/attachments/456/shot.png"><a href="/download/attachments/123/spec.pdf">附件</a><table><colgroup><col></colgroup><tbody><tr><th>名称</th><th>含义</th></tr><tr><td>RN</td><td>原生</td></tr></tbody></table><p>结尾</p>' } },
} });

test("MCP 文件导出保留原位引用、跨页面附件和去重，不需要浏览器", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "confluence-test-"));
  try {
    const image = Buffer.from([137, 80, 78, 71]);
    await writeFile(path.join(root, "456.json"), JSON.stringify({ success: true, data: { contentId: "456", attachments: [
      { filename: "unused.png", size: 1, encoding: "base64", content: "eA==" },
      { filename: "shot.png", size: image.length, encoding: "base64", content: image.toString("base64") },
    ] } }));
    await writeFile(path.join(root, "123.json"), JSON.stringify({ success: true, data: { contentId: "123", attachments: [
      { filename: "spec.pdf", size: 3, encoding: "base64", content: "YWJj" },
    ] } }));
    const plan = planConfluence(page, url);
    expect(plan.resources.length).toBe(2);
    expect(plan.resources[0].contentId).toBe("456");
    const output = path.join(root, "output");
    const result = await exportConfluence(page, url, root, output);
    const markdown = await readFile(result, "utf8");
    const directory = path.join(output, "示例页面");
    expect(result).toBe(path.join(directory, "示例页面.md"));
    expect((await readdir(directory)).sort()).toEqual(["assets", "示例页面.md"]);
    expect(await readdir(path.join(directory, "assets"))).toHaveLength(2);
    expect(await readFile(path.join(directory, "assets", plan.resources[0].localName))).toEqual(image);
    for (const resource of plan.resources) expect(markdown).toContain(`](./assets/${resource.localName})`);
    expect(markdown).toContain("confluence_version: 4");
    expect(markdown).toContain("结尾");
    expect(markdown).not.toContain("目录界面");
    expect(markdown).not.toContain("不得进入正文");
    expect(markdown).toContain("| 名称 | 含义 |");
    expect(markdown).not.toContain("<table");
    expect(markdown.indexOf("图片之前")).toBeLessThan(markdown.indexOf("![]"));
    expect(markdown.indexOf("![]")).toBeLessThan(markdown.indexOf("图片之后"));
    await expect(exportConfluence(page, url, root, output)).rejects.toThrow("EEXIST");
    await writeFile(path.join(root, "456.json"), JSON.stringify({ success: true, data: { contentId: "456", attachments: [
      { filename: "wrong.png", size: 1, encoding: "base64", content: "eA==" },
    ] } }));
    await expect(exportConfluence(page, url, root, path.join(root, "wrong"))).rejects.toThrow("未精确命中");
    await writeFile(path.join(root, "456.json"), JSON.stringify({ success: true, data: { contentId: "456", attachments: [
      { filename: "shot.png", size: 100, encoding: "base64", content: "eA==" },
    ] } }));
    await expect(exportConfluence(page, url, root, path.join(root, "truncated"))).rejects.toThrow("字节数不符");
    expect(await readdir(root)).not.toContain("wrong");
    expect(await readdir(root)).not.toContain("truncated");
  } finally {
    await rm(root, { recursive: true });
  }
});

test("MCP 实例和页面身份不匹配、缺少渲染正文时明确拒绝", () => {
  expect(() => planConfluence(page, url.replace("123", "999"))).toThrow("不匹配");
  expect(() => planConfluence(page, url.replace("example.com", "other.com"))).toThrow("不匹配");
  expect(() => planConfluence('{"success":false,"data":{}}', url)).toThrow("成功");
  expect(() => planConfluence(page.replace('"view"', '"storage"'), url)).toThrow("body.view");
});

test("未加载 Jira 宏保留真实工单链接，移除加载占位文本", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "confluence-jira-test-"));
  const response = JSON.stringify({ success: true, data: {
    id: "123", title: "宏测试", version: { number: 1 }, _links: { base: "https://confluence.example.com" },
    body: { view: { value: '<p>参考<span data-macro-name="jira"><a class="jira-issue-key" href="https://jira.example.com/browse/TEST-1"><span class="issue-placeholder">等待</span>TEST-1</a> - <span class="summary">Getting issue details...</span><span class="issue-placeholder">STATUS</span></span></p>' } },
  } });
  try {
    const result = await exportConfluence(response, url, root, path.join(root, "output"));
    const markdown = await readFile(result, "utf8");
    expect(markdown).toContain("[TEST-1](https://jira.example.com/browse/TEST-1)");
    expect(markdown).not.toContain("Getting issue details");
    expect(markdown).not.toContain("STATUS");
    expect(markdown).not.toContain("等待");
  } finally {
    await rm(root, { recursive: true });
  }
});

test("RustFS 内存转换保留正文与资源且拒绝缺失附件", async () => {
  const { confluenceBundle } = await import('./confluence');
  const plan = planConfluence(page, url);
  const assets = plan.resources.map(item => ({name:item.localName, content:Buffer.from('bytes').toString('base64')}));
  const bundle = confluenceBundle(page,url,assets);
  expect(bundle).toHaveLength(3);
  const markdown = Buffer.from(bundle[0].content,'base64').toString();
  expect(markdown).toContain('confluence_version: 4');
  expect(markdown).toContain('结尾');
  expect(markdown).toContain('./assets/asset-');
  expect(() => confluenceBundle(page,url,[])).toThrow('附件数量');
  expect(() => confluenceBundle(page,url,[assets[0],assets[0]])).toThrow('附件数量');
});

test("合法空白页保留元数据并标记为空，缺正文和不可转换内容仍失败", async () => {
  const { confluenceBundle } = await import('./confluence');
  const response = (html: string) => JSON.stringify({ success: true, data: {
    id: "123", title: "空白页面", version: { number: 1 }, _links: { base: "https://confluence.example.com" },
    body: { view: { value: html } },
  } });
  for (const html of ["<p><br></p>", "", "<p>&nbsp;</p>", "  \n"]) {
    expect(planConfluence(response(html), url).resources).toHaveLength(0);
    const files = confluenceBundle(response(html), url, []);
    const markdown = Buffer.from(files[0].content, "base64").toString();
    expect(files).toHaveLength(1);
    expect(markdown).toContain('title: "空白页面"');
    expect(markdown).toContain('confluence_page_id: "123"');
    expect(markdown).toContain('confluence_version: 1');
    expect(markdown).toContain('confluence_body_empty: true');
    expect(markdown.split('---')[2]?.trim()).toBe('');
  }
  for (const html of ['<iframe src="https://example.com"></iframe>', '<canvas></canvas>', '<div data-macro-name="easy-heading-free">目录内容</div>']) {
    expect(() => confluenceBundle(response(html), url, [])).toThrow('Confluence 转换后正文为空');
  }
  expect(() => confluenceBundle(response('text').replace('"view"', '"storage"'), url, [])).toThrow('body.view');
});

test("Jira 与其他不可本地化资源保留绝对链接，当前实例附件照常下载", async () => {
  const { confluenceBundle } = await import('./confluence');
  const response = JSON.stringify({ success: true, data: {
    id: "123", title: "混合资源", version: { number: 1 }, _links: { base: "https://confluence.example.com" },
    body: { view: { value: '<p>开始</p><img src="/download/attachments/123/shot.png"><img src="https://jira.example.com/images/icons/issuetypes/epic.svg"><img src="/images/icons/static.svg"><img src="https://other.example.com/download/attachments/456/remote.png"><a href="https://cdn.example.com/spec.pdf?version=2">远程附件</a><p>结尾</p>' } },
  } });
  const plan = planConfluence(response, url);
  expect(plan.resources).toHaveLength(1);
  expect(plan.resources.every(item => item.url.startsWith('https://confluence.example.com/download/'))).toBe(true);
  const assets = plan.resources.map(item => ({name:item.localName,content:Buffer.from('bytes').toString('base64')}));
  const files = confluenceBundle(response,url,assets);
  const markdown = Buffer.from(files[0].content,'base64').toString();
  expect(files).toHaveLength(2);
  expect(markdown).toContain('](https://jira.example.com/images/icons/issuetypes/epic.svg)');
  expect(markdown).toContain('](https://confluence.example.com/images/icons/static.svg)');
  expect(markdown).toContain('](https://other.example.com/download/attachments/456/remote.png)');
  expect(markdown).toContain('](https://cdn.example.com/spec.pdf?version=2)');
  for (const resource of plan.resources) expect(markdown).toContain(`](./assets/${resource.localName})`);
  expect(markdown.indexOf('epic.svg')).toBeLessThan(markdown.indexOf('结尾'));
  expect(() => confluenceBundle(response,url,[])).toThrow('附件数量');
});

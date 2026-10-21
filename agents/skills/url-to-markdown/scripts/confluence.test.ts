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

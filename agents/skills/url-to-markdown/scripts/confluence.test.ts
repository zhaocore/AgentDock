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

test("合并单元格展开后保持行列对应，跨行问题名称保留到后续行", async () => {
  const { confluenceBundle } = await import('./confluence');
  const response = JSON.stringify({ success: true, data: {
    id: "123", title: "兼容性问题", version: { number: 4 }, _links: { base: "https://confluence.example.com" },
    body: { view: { value: '<table><thead><tr><th>问题</th><th>描述</th><th>方案</th></tr></thead><tbody><tr><td rowspan="2">音频</td><td>404</td><td>解码</td></tr><tr><td>再次播放失败</td><td>不清除资源</td></tr><tr><td colspan="2"><p>合并说明</p></td><td><br></td></tr></tbody></table>' } },
  } });
  const markdown = Buffer.from(confluenceBundle(response, url, [])[0].content, 'base64').toString();
  expect(markdown).toContain('| 音频 | 404 | 解码 |');
  expect(markdown).toContain('| 音频 | 再次播放失败 | 不清除资源 |');
  expect(markdown).toContain('| 合并说明 |  |  |');
});

test("Rules 表格的段落表头、多段单元格、竖线和空备注保持有效 GFM 行", async () => {
  const { confluenceBundle } = await import('./confluence');
  const response = JSON.stringify({ success: true, data: {
    id: "123", title: "规则表格", version: { number: 1 }, _links: { base: "https://confluence.example.com" },
    body: { view: { value: `<h1>4. Rules</h1><table><colgroup><col></colgroup><thead><tr>
      <th><p>ID</p></th><th><p>Rule command</p></th><th><p>Remark</p></th>
      </tr></thead><tbody><tr><td>1</td><td><p><strong>检查</strong></p><p><code>sum(a|b)</code><br>下一行</p></td><td><br></td></tr>
      <tr><td>2</td><td><a href="/rules">链接</a><img src="/download/attachments/123/shot.png"></td><td><p>&nbsp;</p></td></tr>
      <tr><td><br></td><td><br></td><td><br></td></tr></tbody></table><p>结尾</p>` } },
  } });
  const plan = planConfluence(response, url);
  const assets = plan.resources.map(item => ({ name: item.localName, content: Buffer.from('bytes').toString('base64') }));
  const files = confluenceBundle(response, url, assets);
  const markdown = Buffer.from(files[0].content, 'base64').toString();
  const table = markdown.slice(markdown.indexOf('|'), markdown.indexOf('\n\n结尾'));
  expect(table.split('\n')).toEqual([
    '| ID | Rule command | Remark |',
    '| --- | --- | --- |',
    '| 1 | **检查**<br>`sum(a\\|b)`<br>下一行 |  |',
    `| 2 | [链接](https://confluence.example.com/rules)![](./assets/${assets[0].name}) |  |`,
    '|  |  |  |',
  ]);
  expect(files).toHaveLength(2);
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

test("RustFS 内存转换缺失附件保留原链接，拒绝重复或无效附件", async () => {
  const { confluenceBundle } = await import('./confluence');
  const plan = planConfluence(page, url);
  const assets = plan.resources.map(item => ({name:item.localName, content:Buffer.from('bytes').toString('base64')}));
  const bundle = confluenceBundle(page,url,assets);
  expect(bundle).toHaveLength(3);
  const markdown = Buffer.from(bundle[0].content,'base64').toString();
  expect(markdown).toContain('confluence_version: 4');
  expect(markdown).toContain('结尾');
  expect(markdown).toContain('./assets/asset-');
  const partial = confluenceBundle(page,url,[assets[0]]);
  const partialMarkdown = Buffer.from(partial[0].content,'base64').toString();
  expect(partial).toHaveLength(2);
  expect(partialMarkdown).toContain(`](./assets/${assets[0].name})`);
  expect(partialMarkdown).toContain(`](${plan.resources[1].url})`);
  expect(() => confluenceBundle(page,url,[assets[0],assets[0]])).toThrow('名称重复');
  expect(() => confluenceBundle(page,url,[{name:'unexpected.png',content:'YQ=='}])).toThrow('附件名称');
  expect(() => confluenceBundle(page,url,[{name:assets[0].name,content:'invalid'}])).toThrow('base64');
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
  for (const html of ['<iframe></iframe>', '<canvas></canvas>', '<div data-macro-name="easy-heading-free">目录内容</div>']) {
    expect(() => confluenceBundle(response(html), url, [])).toThrow(`Confluence 转换后正文为空 page_id=123 url=${url}；源页面包含未转换内容`);
  }
  expect(() => confluenceBundle(response('text').replace('"view"', '"storage"'), url, [])).toThrow('body.view');
});

test("无法解析的 iframe 在原位置保留链接，单独嵌入页面也能导出", async () => {
  const { confluenceBundle } = await import('./confluence');
  const response = (html: string) => JSON.stringify({ success: true, data: {
    id: "123", title: "嵌入页面", version: { number: 1 }, _links: { base: "https://confluence.example.com" },
    body: { view: { value: html } },
  } });
  for (const src of ['//docs.google.com/presentation/d/slides/embed', '/embedded/slides']) {
    const html = `<p><iframe src="${src}"></iframe></p>`;
    expect(planConfluence(response(html), url).resources).toHaveLength(0);
    const files = confluenceBundle(response(html), url, []);
    const markdown = Buffer.from(files[0].content, 'base64').toString();
    expect(markdown).toContain(`[iframe](${new URL(src, 'https://confluence.example.com').href})`);
    expect(markdown).not.toContain('confluence_body_empty: true');
    const mixed = confluenceBundle(response(`<p>开始</p>${html}<p>结尾</p>`), url, []);
    const body = Buffer.from(mixed[0].content, 'base64').toString();
    expect(body.indexOf('开始')).toBeLessThan(body.indexOf('[iframe]'));
    expect(body.indexOf('[iframe]')).toBeLessThan(body.indexOf('结尾'));
  }
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
  const missing = confluenceBundle(response,url,[]);
  expect(missing).toHaveLength(1);
  expect(Buffer.from(missing[0].content,'base64').toString()).toContain(`](${plan.resources[0].url})`);
});

test("非 HTTP 资源保留引用且不进入附件计划，HTTP URL 凭据仍拒绝", async () => {
  const { confluenceBundle } = await import('./confluence');
  const inline = 'data:image/png;base64,iVBORw0KGgo=';
  const local = 'file:///private/tmp/missing-confluence-image.png';
  const response = JSON.stringify({ success: true, data: {
    id: "123", title: "资源协议", version: { number: 1 }, _links: { base: "https://confluence.example.com" },
    body: { view: { value: `<p>开始</p><img src="${inline}"><img src="${local}"><img src="/download/attachments/123/shot.png"><p>结尾</p>` } },
  } });
  const plan = planConfluence(response, url);
  expect(plan.resources).toHaveLength(1);
  expect(plan.resources[0].filename).toBe('shot.png');
  const assets = plan.resources.map(item => ({ name: item.localName, content: Buffer.from('bytes').toString('base64') }));
  const files = confluenceBundle(response, url, assets);
  const markdown = Buffer.from(files[0].content, 'base64').toString();
  expect(files).toHaveLength(2);
  expect(markdown).toContain(`](${inline})`);
  expect(markdown).toContain(`](${local})`);
  expect(markdown).toContain(`](./assets/${plan.resources[0].localName})`);
  expect(markdown).toContain('结尾');
  const authenticated = response.replace(inline, 'https://user:pass@cdn.example.com/image.png');
  expect(() => planConfluence(authenticated, url)).toThrow('不得包含用户名或密码');
  expect(() => planConfluence(response, url.replace('https://', 'https://user:pass@'))).toThrow('不得包含用户名或密码');
});

test("不可解析的正文链接和媒体引用保留原值，后续正常资源仍可导出", async () => {
  const { confluenceBundle } = await import('./confluence');
  for (const invalid of ['resCors://enable localhost:4200/infiniteStage/remoteEntry.js', 'https://[bad']) {
    const response = JSON.stringify({ success: true, data: {
      id: "123", title: "无效引用", version: { number: 1 }, _links: { base: "https://confluence.example.com" },
      body: { view: { value: `<p>开始</p><a href="${invalid}">原链接</a><img data-image-src="${invalid}"><video src="${invalid}" poster="/download/attachments/123/poster.png"></video><video poster="${invalid}"></video><object data="${invalid}">对象</object><img src="/download/attachments/123/valid.png"><a href="/pages/next">下一页</a><p>结尾</p>` } },
    } });
    const plan = planConfluence(response, url);
    expect(plan.resources.map(item => item.filename).sort()).toEqual(['poster.png', 'valid.png']);
    const assets = plan.resources.map(item => ({ name: item.localName, content: Buffer.from('bytes').toString('base64') }));
    const files = confluenceBundle(response, url, assets);
    const markdown = Buffer.from(files[0].content, 'base64').toString();
    expect(files).toHaveLength(3);
    const destination = invalid.includes(' ') ? `<${invalid}>` : invalid;
    expect(markdown).toContain(`[原链接](${destination})`);
    expect(markdown).toContain(`![](${destination})`);
    expect(markdown).toContain(`![poster](${invalid})`);
    expect(markdown).toContain(`[object](${invalid})`);
    expect(markdown).toContain('[下一页](https://confluence.example.com/pages/next)');
    for (const asset of assets) expect(markdown).toContain(`](./assets/${asset.name})`);
    expect(markdown).toContain('结尾');
  }
});

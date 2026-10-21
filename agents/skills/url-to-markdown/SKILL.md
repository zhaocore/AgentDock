---
name: url-to-markdown
description: 将 URL 网页保存为 Markdown，Confluence 链接优先通过可用的读取 MCP 导出正文和附件，其他网页支持按域名配置 Cookie。目录与 Markdown 按标题命名，资源保存到 assets 并原位替换相对链接。用于网页剪藏、离线阅读和登录态文档导出。
---

# URL 转 Markdown

先将本文件所在目录确定为 `SKILL_DIR`。所有命令使用实际绝对路径，不依赖当前工作目录。需要 Bun；仅浏览器抓取路径需要 Chrome。依赖只安装到技能目录。

## 先选择读取路径

输入 URL（不区分大小写）含 `confluence`：先发现宿主可用的 Confluence 读取 MCP，核对目标实例，按 [Confluence MCP 导出](references/confluence.md) 读取正文与资源。MCP 可用时优先使用，不先启动浏览器或索要 Cookie。CLI 本身不能发现、调用编码代理宿主的 MCP 工具；由代理编排读取，再把响应文件交给本技能脚本。

没有匹配实例的读取 MCP 时，明确说明能力缺失，使用下方 Cookie 浏览器流程；已有 MCP 但权限失败、内容为空或附件错配时明确报错，不能静默改用网页抓取掩盖失败。非 Confluence URL 使用原有浏览器流程。

```bash
bun install --cwd "${SKILL_DIR}"
```

默认使用系统 Chrome；没有 Chrome 时执行 `bunx --cwd "${SKILL_DIR}" playwright install chromium`，运行时设置 `URL_BROWSER_CHANNEL=chromium`。可通过 `URL_CHROME_PATH` 指定浏览器可执行文件。

## Cookie 配置

首次使用复制 `cookies.example.json` 为 `cookies.local.json`，填写浏览器请求头中的 Cookie 字符串（不带 `Cookie:` 前缀），权限设为 `600`。禁止把真实 Cookie 写进 SKILL.md、示例、命令行或输出文档；不得读取并展示配置值。

```json
{
  "cookies": [
    { "domain": "docs.example.com", "cookie": "session=替换为真实值; token=替换为真实值" }
  ]
}
```

`docs.example.com` 仅精确主机，`.example.com` 显式允许主域及子域；不填协议、端口或路径。仅向对应域发送 Cookie，跨域 CDN 需要自己的配置。HTTPS 页面配置为 secure Cookie，HTTP 页面仅允许当前主机的 Cookie；不向 HTTP 子域扩散 HTTPS Cookie。Cookie 仅配置名称和值，路径统一 `/`，不恢复 localStorage 或浏览器完整会话。未配置的域按公开页面访问。

默认读技能目录的 `cookies.local.json`；不存在时按公开页面抓取。`--config` 指定文件时，文件必须存在。空配置使用 `{"cookies":[]}`。

## 执行

```bash
bun "${SKILL_DIR}/scripts/main.ts" 'https://docs.example.com/article'
bun "${SKILL_DIR}/scripts/main.ts" 'https://docs.example.com/article' --config /private/path/cookies.json --output-dir /tmp/article-export --timeout 60000
```

`--output-dir` 指定导出根目录，可已存在；脚本在其中创建 `<标题>/`，已有同名标题目录则明确报错，禁止覆盖。默认导出根目录为 `SKILL_DIR/output/<域名>/`。标题保留中文与空格，非法路径字符替换为 `-`，去除首尾空格及末尾点号；Windows 保留名称加 `_` 前缀。空标题或清理后为空明确报错。元信息保留原始标题。终端打印 Markdown 绝对路径，标题目录可整体移动：

```text
article-export/
  页面标题/
    页面标题.md
    assets/
      asset-<URL哈希>.png
      asset-<URL哈希>.pdf
```

Markdown 含源 URL、标题、捕获时间。正文优先取 `article`、`main`、`[role=main]`，否则取 body；保留表格、代码块、正文顺序。图片使用渲染后的 currentSrc，兼顾常见 data-src 懒加载。下载图片、video/audio/source/track/object/embed 引用、download 链接和常见附件扩展名链接，去重后放标题目录的 `assets/`；正文原位置改为 `./assets/...` 相对链接。网页链接改为绝对在线 URL，锚点保留。不递归抓网页，不下载布局 CSS/JS、字体或 CSS 背景图。

页面滚动触发懒加载，最多 30 屏，随后捕获。超长文章、虚拟列表、需点击展开、加密媒体、blob 资源或自定义登录流程需要针对站点另行处理；不要把登录页当成功导出。401/403 明确失败，HTTP 200 登录页需核对实际标题和正文。

下载失败会警告并重试，共 3 次；最终失败返回非零，清理本次创建的输出目录。不得忽略失败、留下远程图片冒充完整离线文档。输出包含登录后私有内容，按源站权限保存与分享。

## 验证

```bash
bun run --cwd "${SKILL_DIR}" typecheck
bun run --cwd "${SKILL_DIR}" test
```

本仓库 Go Agent 当前只加载技能正文，尚无 shell/浏览器执行工具。此技能须由可执行 CLI 的编码代理使用，不宣称 dutybotGo 会话已能自动运行脚本。

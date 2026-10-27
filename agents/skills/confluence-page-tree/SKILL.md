---
name: confluence-page-tree
description: 导出 Confluence 左侧完整页面树为本地层级 JSON，包含标题、链接、页面版本、每节点最后查看时间与最后修改时间；与上次成功快照比较新增、修改、删除节点并输出 diff。使用本地配置注入 Cookie，适用于页面目录同步及更新检查。
---

# Confluence 页面树

将本文件目录确定为 `SKILL_DIR`。Python 3.10+，仅标准库，无需安装依赖。使用绝对路径执行；不依赖 Chrome Bridge 运行。

## 配置与运行

复制 `config.example.json` 为 `config.local.json`，设置权限 `600`。填写 `base_url`、`page_url` 和完整请求头 Cookie 字符串（不带 `Cookie:` 前缀）。默认示例指向用户提供的 Shopee 页面。

Cookie 从浏览器 DevTools → Network → 已成功的 Confluence 请求 → Request Headers → Cookie 获取，直接在本地配置编辑器填写。HttpOnly 登录 Cookie 无法从 `document.cookie` 读取，不要用统计 Cookie 冒充登录凭据。禁止打印配置或在命令行、技能正文、示例、版本控制和导出结果中写入真实 Cookie。

```bash
cp "${SKILL_DIR}/config.example.json" "${SKILL_DIR}/config.local.json"
chmod 600 "${SKILL_DIR}/config.local.json"
python3 "${SKILL_DIR}/scripts/export_tree.py" --config "${SKILL_DIR}/config.local.json" --output-dir "${SKILL_DIR}/output"
```

导出目标为该页面左侧的**整棵页面树**，包括兄弟页面和所有折叠分支，不限当前页面后代。页面正文、附件及草稿不抓取。通过 `base_url` 支持实例上下文路径，例如 `https://host/wiki`。Cookie 只发送至配置 HTTPS 实例，重定向明确报错。

`config.local.json` 与 `output/` 已忽略。独立指定其他配置路径时，调用方负责忽略文件并限制权限。不同实例、空间或树范围用不同输出目录；禁止拿不匹配的基线比较。

## 结果与差异

每次成功产生 `<UTC时间>.tree.json`、`<UTC时间>.diff.json`，最后原子更新 `latest.json` 作为下次基线。终端只输出文件路径和节点数量。首次 diff 为 `initial: true`，全部节点列入 `added`。后续无变化时三个差异数组均为空。失败不推进基线；保留历史文件。

树 JSON 的 `nodes` 为原侧边栏顶层数组，节点形态：

```json
{
  "id": "2701470185",
  "title": "[Studio][FE2 team] 团队空间",
  "url": "https://confluence.shopee.io/pages/viewpage.action?pageId=2701470185",
  "version": 1,
  "last_viewed_at": "2026-10-21T09:00:00+00:00",
  "last_modified_at": "2025-04-03T10:00:00+08:00",
  "children": []
}
```

`children` 递归保留原层级，数组按 Confluence `position` 排序。`version` 为发布版本号，用于识别发布更新，不识别草稿。

`last_viewed_at` 为该节点的页面元数据成功读取并校验后的 UTC 时间，每个节点单独记录；它表示本技能最近读取该页面的时间，不表示用户在 Confluence 浏览器中的打开历史。`last_modified_at` 为 Confluence `version.when` 返回的最后发布修改时间，保留服务器时区。两者为含时区的 ISO 8601 字符串；接口缺少或返回无效修改时间时明确失败，不用抓取时间代替。

diff 以稳定页面 ID 匹配，包含：

- `added`、`deleted`：节点标题、URL、版本、最后查看时间、最后修改时间、父节点 ID 与同级位置。
- `modified`：`id`、变化的 `fields`、`before`、`after`；版本、最后修改时间、重命名、父级移动及同级排序变化均归此类。
- `initial`、前后抓取时间及实例/空间/根范围。抓取时间、最后查看时间本身不算节点变化，避免每次读取将全部节点误报为修改。

新快照和 diff 使用 `schema_version: 2`。仍可与旧版 `schema_version: 1` 基线比较：旧节点未知的两个时间在 diff 的历史记录中为 `null`，不推算或回填；首次取得修改时间不单独触发修改，版本和结构变化仍正常识别。旧版统计字段不再输出，历史文件保留原样。

删除代表节点从当前账号可见树消失，可能是删除、权限变更、归档或移出范围，不能证明页面已被物理删除。整棵树并非服务器事务快照；抓取过程中有编辑或移动时重新导出确认。抓取期间发现树标题与 REST 标题不一致会明确失败。

## 站点契约与故障

已通过 Chrome Bridge 检查 Shopee Confluence 7.13.7：`.plugin_pagetree` 的 hidden inputs 提供 `treeRequestId`、`spaceKey`、`noRoot`、`rootPageId`；标题位于 `.plugin_pagetree_children_span`，ID 为 `childrenspan<pageId>-<treeId>`。`naturalchildren.action` 返回一层有序 `ul/li`，`data-type=toggle` 标记可展开节点。版本及最后修改时间读取 `/rest/api/content/<id>?expand=version` 的 `version.number`、`version.when`。

脚本以最多 10 个节点并行读取元数据和折叠子目录，完成顺序不影响原始父子层级及 position 顺序；工作线程不递归等待子任务。每次请求使用独立 HTTP opener，避免线程间共享处理器。大空间耗时与节点数、远程延迟和重试有关。网络错误、429/5xx 最多尝试三次并警告，401/403、登录跳转、HTML 结构或 JSON 校验失败直接终止。不要将空页面、错误页面或部分树当成功结果。错误输出不包含 Cookie 或原始响应正文。

`.export.lock` 防止同目录并发导出；进程被强制终止后，仅确认没有正在运行的导出进程时清理遗留锁。已有 `latest.json` 损坏时先检查并保留原件，不静默重建基线。

本仓库 Go Agent 只加载技能正文，尚无 CLI 执行工具；由具有终端能力的编码代理运行脚本。

## 验证

```bash
python3 -m unittest discover -s "${SKILL_DIR}/scripts" -p 'test_*.py'
```

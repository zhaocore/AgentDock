---
name: repair-markdown-tables
description: 批量修复 url-to-markdown 历史导出 Markdown 表格，支持离线预检和修复、按 Confluence 原版本恢复复杂表格，并重建清单准备 ZIP。保留附件与备份，压缩包准备完成即结束，不上传或导入。
---

# 修复已导出的 Markdown 表格

使用本技能目录中的 `scripts/repair_tables.py`，需要 Python 3.10+，无第三方依赖。输入一个明确的 Markdown 文件或目录；目录递归处理 `.md` 文件。用户只要求创建技能时，不自动覆盖整库；实际处理范围以用户指定为准。

```bash
python3 /absolute/path/to/repair-markdown-tables/scripts/repair_tables.py '/path/to/export-root'
```

脚本原地修复，并为每个变化文件创建同目录 `<原文件名>.bak`，不覆盖已有备份。先分析全部文件，再写入；任何表格列数不明确、符号链接或备份冲突都会明确失败。写入中出现文件系统错误时立即停止，已修改文件可从 `.bak` 恢复。输出 JSON 包含扫描量、修改量、修复表格数与文件路径，不打印正文。

仅处理已有 `| --- |` 分隔行、外侧竖线完整、列数可确定的表格。断行单元格拼回同一物理行，内部换行变为 `<br>`，保留空列、原有转义、行内格式及链接。正常表格行、front matter、围栏和缩进代码块、其他正文与 assets 均保持不变。重复执行应报告零修改。

表头列数与分隔行必须一致；正文允许 GFM 已闭合的短行，不因单元格少于表头就误报缺少闭合竖线。历史导出中已丢失的 rowspan/colspan 不能从短行唯一恢复：离线修复保留现有短行，不猜测缺失列位置；需恢复原始行列关系时读取对应版本的源 HTML，再用修复后的 url-to-markdown 转换。文件写入前必须通过重复运行零变化检查；正文中的短横线数据行不能被误当成新表格。

无法从 Markdown 唯一恢复的裸 `|`、已丢失的合并位置等内容不猜测；使用下方同版本源正文恢复。HTML 表格及其内部代码受保护，不按 GFM 行再次修复。

## 完整预检与源版本恢复

先预检整个目录，保存所有可修复项目和失败原因，不只报告第一个失败文件：

```bash
python3 /absolute/path/to/repair-markdown-tables/scripts/inspect_tables.py \
  '/path/to/confluence-library' '/path/to/inspection.json'
```

若清单中 `unresolved` 非空，且用户要求继续处理这些文件，按源页面相同版本恢复正文，不能仅把已覆盖的少数类型修好就宣称全量完成。需要 Bun、项目依赖及既有 `CONFLUENCE_BASE_URL`、`CONFLUENCE_COOKIE` 环境配置；凭据不得写入命令或报告。仓库中的命令：

```bash
bun --env-file=/absolute/path/to/dutybotGo/.env \
  /absolute/path/to/dutybotGo/agents/skills/repair-markdown-tables/scripts/repair_from_source.ts \
  '/path/to/inspection.json' '/path/to/source-results.json'
```

该命令只读取清单内页面，每批最多 10 页；按原元数据的页面 ID 和版本读取 `body.view`，身份或版本不符明确失败，禁止改用最新版本冒充原版本。复用 url-to-markdown 转换器恢复正文，保留原 front matter 和现有附件字节；已有附件继续使用原本地相对路径，原先未下载的附件保留源链接。先备份，再原子替换，原备份与当前原文相同时可复用而不覆盖，不同时报冲突。源 HTML 仅在内存处理，不落盘、不打印正文或凭据。

转换结果必须先通过离线零变化复核，逐文件输出结果并保存进度；存在源读取、版本、备份等失败时返回非零并列清楚原因，不能把读取失败的页面计入已修复。复杂表格保留完整 HTML 结构，阅读器须支持内嵌 HTML。源恢复后重新预检整个目录，再对 `planned` 中确定可修复的文件运行离线修复；最终清单须区分已修复、无需改动及仍失败的文件，不能沿用旧错误清单。

完成后核对修改文件、备份数量及重复运行零修改，并用支持 GFM 的阅读器抽查表格。

## 修复后准备压缩包

输入须为含原始 `confluence-library.json` 的完整文件库根目录，不能只传某个页面或随意缺少附件。需要保留本技能所在 dutybotGo 仓库及 `go.mod`，安装项目要求的 Go 工具链；打包器直接复用仓库 Go 归档和导入校验，不重复实现清单规则。

```bash
python3 /absolute/path/to/repair-markdown-tables/scripts/repair_and_pack.py \
  '/path/to/confluence-library' '/path/to/confluence-library-repaired.zip'
```

命令先构建打包器，随后执行修复；修复成功才重算清单内每个文件的大小、SHA-256 和导出时间，流式写入 ZIP，再在本地运行与服务端导入相同的格式及完整性校验，通过后发布到新的输出路径。校验只读取本地文件，不请求远程导入接口。已修复目录也可执行，零修复不影响打包。只包含原清单列出的 Markdown 与附件，不把 `.bak`、`.DS_Store`、脚本或其他额外文件加入归档。缺清单、缺文件、符号链接、身份/版本不匹配、超过现有限额或输出已存在均报错；失败不发布最终 ZIP，修复阶段已产生的改动和备份仍保留。

压缩包准备完成即结束，返回 ZIP 绝对路径、文件大小及修复结果。不得继续上传、触发远程导入预览、确认导入或发布；用户后续自行决定是否导入。

原目录中的清单不覆盖；新清单仅写入新 ZIP。页面来源、版本与 `modified_at` 保留，不以本地修复时间伪装源页更新。用户后续导入已有同时间页面且内容不同时，预览仍按原规则显示冲突并保留现有内容；生成合法 ZIP 不代表这些页面会覆盖成功。

若用户只需将已经手动修改的目录重新打包，可在仓库中直接执行：

```bash
go -C /absolute/path/to/dutybotGo run ./agents/skills/repair-markdown-tables/scripts \
  '/path/to/confluence-library' '/path/to/confluence-library-repaired.zip'
```

验证：

```bash
python3 -m unittest discover -s /absolute/path/to/repair-markdown-tables/scripts -p 'test_*.py'
go -C /absolute/path/to/dutybotGo test ./agents/skills/repair-markdown-tables/scripts
```

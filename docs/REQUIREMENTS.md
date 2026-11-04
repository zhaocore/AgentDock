# AgentDock 需求文档

本文件是仓库唯一权威需求文档（活文档）。行为变化或需求澄清后必须同步更新本文件；规则见 `AGENTS.md`。

层级约定：一级章节为模块/能力，二级小节为功能，列表项为验收细节。状态标注：**[已实现]**、**[部分实现]**、**[未实现]**。

> 本文档描述 AgentDock 的目标形态（自 GraphGo 改造而来）。2026-10-16 改造主链路已完成：基础设施、仓库、Agent、实例、会话、前端与 swagger 均已实现，标注同步更新。

## 1. 产品定位

- 产品展示名统一为 `AgentDock`，项目根目录为 `AgentDock/`；Go module、前端模块、同源代理路径、Cookie、构建产物与示例资源名使用 `agentdock`，环境变量品牌前缀使用 `AGENTDOCK`。源码、配置、技能与历史文档同步更名，不保留旧代理路径；升级后用户重新登录。
- 已部署数据库、账户、对象桶及 Docker 持久卷不随品牌更名迁移或删除；已有部署须显式保留连接配置与原 Compose 项目名，新部署默认使用新品牌资源名。仅迁移文件中的品牌注释同步更名，SQL、编号、任务载荷、用户隔离与 Agent 运行时边界保持不变。
- AgentDock：多用户 Go Agent 管理平台。部署者通过目录预设公共 Agent，所有用户共用 skills、MCP 与模型配置，各预设仅角色系统提示词不同；用户管理自己的实例和会话。
- MySQL 存业务数据与会话历史，Redis 用于 Asynq 任务队列与缓存；Confluence 导出内容使用 RustFS（S3 兼容对象存储）。不使用 PostgreSQL、Neo4j 或向量检索。
- HTTP 服务与后台 Worker（Asynq）同进程运行；Agent 实例以服务进程内长驻 goroutine 形式运行。
- 当前状态：改造主链路完成；MCP 工具桥接与 subagent 工具化为提示词注入实现，待接入 langchaingo agent 循环。

## 2. 用户与认证 **[已实现]**

### 2.1 注册

- `POST /v1/auth/register`：邮箱 + 密码注册，密码 bcrypt 存储，注册成功即签发 JWT。
- 邮箱唯一。

### 2.2 登录

- `POST /v1/auth/login`：校验密码后签发 JWT。
- JWT 有效期由 `AUTH_TOKEN_EXPIRY` 控制，默认 `24h`。

### 2.3 修改密码

- `POST /v1/auth/change-password`：修改当前登录用户密码。

### 2.4 当前用户

- `GET /v1/auth/me`：返回当前登录用户信息。

## 3. 资源隔离模型 **[已实现]**

- 多用户平台。仓库（repositories）为全局共享资源：所有登录用户可见、可用；仓库的创建与删除需登录（写操作仍受 API token `can_write` 约束）。
- 公共 Agent 定义由部署者维护，全部登录用户可读、可创建自己的实例，普通用户和 API token 均不可修改或删除公共定义。原有私有 Agent 继续按 owner 隔离，保留其数据与 API 兼容性。
- 实例归当前登录用户；公共 Agent 不共享运行实例、会话或消息。
- Agent 会话（sessions、messages）按用户隔离：任何用户只能看到自己的会话与消息，即使会话引用共享仓库或他人创建的 Agent 定义。
- API token 沿用 `gk_` 前缀机制；token 即租户身份，鉴权后复用同一 `user_id` 上下文键，隔离规则与 JWT 用户一致。

## 4. 仓库管理 **[已实现]**

### 4.1 数据模型

- 表 `repositories`：`id`、`name`（唯一）、`git_url`、`repo_type`（`frontend` / `backend` / `rn`）、`branch`（默认 `main`）、`local_path`（克隆后绝对路径）、`status`（`pending` / `cloning` / `ready` / `failed`）、`last_error`、时间戳。
- clone 根目录由环境变量 `REPO_ROOT_DIR` 指定，默认为服务进程工作目录下 `repo/` 目录。对象目录按仓库名隔离：`<REPO_ROOT_DIR>/<repo_name>`。

### 4.2 添加仓库

- `POST /v1/repositories`：`name` + `git_url` + `repo_type` + `branch`。创建即返回 201，状态置 `pending`。
- 后台 Asynq 任务执行 `git clone --branch <branch> <git_url> <REPO_ROOT_DIR>/<name>`；clone 成功后检测 `<local_path>/.codegraph` 不存在时执行 `codegraph init -i`（服务器已装 codegraph CLI，Go 只负责触发）。
- 私有仓库访问依赖服务器上已配置的 git 凭据（deploy key / credential helper），平台不在数据库保存任何 git 凭据。
- 任务结果回写：成功 → `ready` + `local_path`；失败 → `failed` + `last_error`。
- 重复添加同名仓库返回 409。

### 4.3 列表与详情

- `GET /v1/repositories`：全局共享，所有用户可见；分页参数 `limit`（默认 20，范围 [1,100]）、`offset`，响应为分页包装。
- `GET /v1/repositories/:id`：含状态、最近错误。

### 4.4 同步与删除

- `POST /v1/repositories/:id/sync`：触发 `git fetch + reset --hard origin/<branch>`（202），状态机同 4.2（重新进入 `cloning` → `ready`/`failed`）。
- `DELETE /v1/repositories/:id`：删除记录并移除本地目录（含 `.codegraph`）。

## 5. Agent 管理 **[部分实现]**

### 5.1 数据模型

- 表 `agents`：`id`、`owner_user_id`、`name`、`system_prompt`、`model`（模型标识）、`skills`（JSON 数组，skill 名列表）、`mcp_servers`（JSON，MCP server 连接配置：名称、传输方式、endpoint/命令、env）、`subagents`（JSON 数组：子 Agent 名称 + 简述 + 可用工具，供主 Agent 作为工具调用）、时间戳。
- Agent 主 SDK 为 langchaingo（`github.com/tmc/langchaingo`）。
- 公共定义的 `owner_user_id` 为 NULL；新增迁移，不修改已应用文件。`enabled` 用于停用已从部署目录移除的预设，保留定义、实例及历史。私有记录保持原 owner。

### 5.2 Agent CRUD

- `POST /v1/agents` / `GET /v1/agents`（公共预设 + 当前 owner 私有定义、分页）/ `GET /v1/agents/:id` / `PUT /v1/agents/:id` / `DELETE /v1/agents/:id`。
- 私有定义仅 owner 可操作；非 owner 访问返回 404。公共定义全用户可读，PUT/DELETE 返回 403；新预设由部署目录生成，不通过普通用户的 CRUD 创建。

### 5.3 Agent 实例

- `POST /v1/agents/:id/instances`：创建运行实例，可指定绑定的仓库 ID 列表（可选）。实例为服务进程内长驻 goroutine，由实例管理器统一注册与生命周期管理。
- `POST /v1/instances/:id/start` / `POST /v1/instances/:id/stop`：启停。
- `GET /v1/agents/:id/instances`：实例列表与状态（`created` / `running` / `stopped` / `failed`）。
- 服务重启后实例记录仍在，状态置 `stopped`，可手动重新 start（会话历史已持久化，恢复语义为新建运行上下文）。
- 停止实例不影响其会话历史。

### 5.4 会话

- `POST /v1/instances/:id/sessions`：在实例上开启新会话，返回 `session_id`。
- `POST /v1/sessions/:id/messages`：发送用户消息；服务端调用 langchaingo agent（携带系统提示词、skills、MCP 工具、subagent 定义），完整回复与中间步骤持久化后返回。
- 会话历史持久化到 MySQL：表 `sessions`（`id`、`user_id`、`instance_id`、`agent_id`、标题、时间戳）与 `messages`（`id`、`session_id`、`role`、`content`、`agent_meta` JSON（中间步骤/token 用量等）、时间戳）。
- `GET /v1/sessions`：当前用户的会话列表（分页，按更新时间倒序）；`GET /v1/sessions/:id/messages`：消息历史。
- 会话按 `user_id` 隔离；`DELETE /v1/sessions/:id` 为软删除，列表与详情均按 `user_id` 过滤。

### 5.5 公共配置与预设 **[已实现]**

- `AGENT_CONFIG_DIR` 指向部署者维护的目录，默认 `agents/`。`skills/<name>/SKILL.md` 存放公共技能正文，`mcp.json` 保存统一 MCP 配置数组，`presets/agent1.md`、`presets/agent2.md` 等文件存放各角色提示词。模型统一使用 `LLM_MODEL`。
- 目录为唯一来源：启动时同步，管理页刷新（GET Agent 列表）重新加载并事务同步。先校验整套配置，失败明确报错，禁止部分更新。预设文件名决定稳定 ID；改正文不改变实例关联。
- 同步后下一轮消息使用新系统提示词和技能正文；不改历史消息，不中断已发出的请求。配置文件删除仅停用公共定义，不级联删除历史。
- MCP 的 endpoint、command、env 等连接细节不得通过公共列表和详情泄露。此任务同步配置与提示词；实际 MCP 工具调用和 subagent 调度仍未实现，界面必须如实提示。
- 运行时边界变化限于从部署目录加载公共配置与技能正文、同步到既有 Agent 运行时；不新增外部依赖或外部服务。

### 5.6 URL 转 Markdown 技能

- `agents/skills/url-to-markdown/` 提供独立 CLI，按域名配置 Cookie，浏览器渲染 URL 后输出 Markdown；真实 Cookie 仅放忽略的本地配置文件，不进入共享技能正文。
- 两条导出路径统一输出 `<标题>/<标题>.md` 与 `<标题>/assets/`，正文资源原位置替换为 `./assets/...` 相对链接。`--output-dir` 指定导出根目录，脚本在其中创建标题目录；已有同名标题目录明确报错。标题保留中文及空格，文件系统非法字符替换为 `-`，空标题或无法形成安全文件名的标题明确报错；元信息保留原始标题。
- 普通网页链接保留在线地址，不递归抓站，不下载页面 CSS/JS。
- 简单表格输出有效 GFM：每行占一个物理行，单元格内段落与换行转为 `<br>`，竖线转义，空单元格保留列位置；表头、行序、链接、图片与行内格式保持原意。示例代码仅在源 HTML 标记为代码时作为代码转换，不猜测普通段落的语言。
- HTML 表格合并单元格必须展开后再转换：rowspan 在覆盖行重复保留内容，colspan 内容放首列、其余覆盖列留空，后续单元格不得错列。历史 Markdown 已闭合的正文短行是合法 GFM，不得误报为缺少竖线；离线转换不猜测已丢失的合并位置，恢复原位置需核对源 HTML。修复结果写入前必须通过重复运行零变化检查，表格正文中的短横线行不得被当成另一张表。
- 上述 GFM 展开仅用于简单表格。含列表、代码块、标题、引用、嵌套表格或多行表头的复杂表格保留完整 HTML 与原合并结构，代码换行编码保留，避免强制行内化丢失语义；HTML 表格不再被离线脚本当作 GFM 解析，阅读器须支持内嵌 HTML。
- 历史修复须提供完整只读预检报告及显式源版本恢复命令。对离线不可唯一还原的文件，复用既有 Confluence 只读 API 获取原页面 ID 对应版本的渲染正文，校验身份与版本后复用转换器恢复；保留原元数据、附件及备份，源 HTML 不落盘，不猜测最新版本，不上传或导入。每批最多 10 页，读取有限重试，逐文件记录结果，错误明确返回。最终重新预检全目录，不能沿用旧失败清单。此能力仅为技能 CLI，不改变 Go Agent 运行时或新增外部依赖。
- `repair-markdown-tables` 技能离线递归修复历史 `.md` 中列数明确的断行表格；仅变化文件原地修改并生成不可覆盖的 `.md.bak`，重复执行零变化。保留正常表格、代码、元数据、附件和其他正文；列数歧义或备份冲突明确失败，分析阶段不写入。完整文件库目录支持修复后重新打包：基于原始清单重算文件大小、SHA-256 与导出时间，仅打包清单内 Markdown 和附件，排除备份；复用 Go 归档及导入校验，通过后才发布新 ZIP，拒绝覆盖已有输出。原目录清单及页面来源、版本、最后修改时间保持不变；不改变现有同时间内容冲突规则，不自动上传或确认导入，不新增依赖或改变 Agent 运行时。
- 修复打包流程在本地 ZIP 准备完成后结束，返回压缩包路径、大小及修复结果。完整性校验仅在本地执行，不触发远程上传、导入预览、确认导入或发布；是否导入由用户后续自行决定。
- 正文链接、图片和媒体（含 poster）中不可解析的 URL 保留原始引用，不标记为下载附件、不发起资源读取，不中断其余正文与合法资源的导出；页面输入 URL 和实例 base 仍按原规则严格校验。
- Cookie 按精确主机匹配，显式点前缀域名允许子域；跨域资源和重定向不向无关域发送 Cookie。资源下载失败重试后明确报错，不生成声称完整的 Markdown。
- 技能独立依赖 Playwright、LinkeDOM、Turndown 与 GFM 插件，仅用于 CLI 浏览器/HTML 接缝，不改变 Go Agent 运行时或新增服务端执行工具。
- 输入 URL 含 `confluence` 时，编码代理先探测可读取该实例的 Confluence MCP；可用时读取 `body.view` 渲染 HTML 与版本，按正文引用通过 MCP 下载附件，再导出 Markdown。MCP 调用由编码代理编排，CLI 不硬编码 MCP 凭据或伪装可直接调用宿主工具。无匹配 MCP 时明确说明，再使用原有 Cookie 浏览器路径。
- Confluence 附件按正文引用的页面 ID、文件名定位，不依赖默认分页附件列表；原始二进制保存在标题目录的 assets/，重复资源去重。独立 CLI 对宿主提供的附件响应严格校验身份、编码与字节数；后台资源下载异常按后台导出的资源规则保留原链接。

## 6. 后台 Worker **[已实现]**

- Asynq 任务类型：`repo:clone`、`repo:sync`、`repo:delete`。任务入队即返回 202，由 Worker 执行并回写状态。
- git 操作通过 `exec.Command` 调用系统 `git` 与 `codegraph` CLI；命令失败时错误信息（stderr 摘要）写入 `last_error`。

## 7. API 令牌 **[改造自已实现]**

- 沿用 `gk_` 前缀 API token：哈希存储、范围（scope）机制、`can_write` 写权限。
- 令牌可调用：仓库管理、Agent 管理、实例与消息、会话历史（自己的）等开放 API。
- 令牌禁止访问：`/auth/*`、`/api-tokens/*`、`/dashboard/summary`。
- 资源映射变化（原知识库 scope 改为仓库/Agent scope）在实现时同步更新 `api_token_scope`。

## 8. 管理后台（dashboard） **[已实现]**

- 项目名与界面全面改为 AgentDock；移除知识库、文档、导入、图谱、查询等模块。
- 页面模块：
  - 登录/注册（保留现有实现）。
  - 仓库管理：列表（全局共享）、添加表单（名称、Git URL、类型、分支）、状态与错误展示、同步/删除。
  - Agent 管理：公共预设及自己的旧私有 Agent 列表、系统提示词预览、公共技能与 MCP 名称、配置刷新、自己的实例启停；公共定义只读，移除逐 Agent 重复配置表单。明确提示 MCP 工具调用尚未接入。
  - 会话历史：由查询历史模块改造为 Agent 会话历史（会话列表 + 消息详情，按当前用户隔离）。
  - API token 管理：保留现有实现。
- 前端规则沿用 AGENTS.md：Next.js 同源代理、HttpOnly Cookie、Vitest + Playwright、稳定 test-id。

## 9. 非目标

- 不做文档导入流水线、向量检索、知识图谱（相关代码全部移除）。
- 不做 Agent 进程级隔离（子进程/容器）。
- 不做 git 凭据管理（依赖服务器已配置的凭据）。
- 不做跨用户会话共享。

## 10. 迁移说明

- 原 GraphGo 的 PostgreSQL / Neo4j / RustFS 依赖与数据不迁移；migrations 目录按新 schema 重写（已应用文件随本次重构整体替换，以 `0001_init` 重新开始编号）。
- 原 `internal/pipeline`、`internal/query`、`internal/store/postgres`、`internal/store/neo4j`、`internal/store/rustfs`、`internal/llm` 全部删除。
- CLI（`cli/go`、`cli/node`、`cli/python`）全部删除；`cmd/graphgo` CLI 入口一并移除，仅保留 `cmd/server` 与 `cmd/migrate`。

## 11. Confluence 页面树技能

### 11.1 独立导出与差异

- `agents/skills/confluence-page-tree/` 提供 Python 标准库 CLI，以页面 URL 定位侧边栏，递归读取完整页面树（含折叠节点），保留原父子层级和 position 顺序。
- 每节点包含 page ID、title、Confluence URL、version、last_viewed_at、last_modified_at 及 children，不输出修改次数；完整保留标题链接内文本，包括前导不换行空格。版本变化用于判断发布更新，不能识别未发布草稿。
- last_viewed_at 为每个节点元数据成功读取并校验后的 UTC 时间，表示本技能最后读取该页面的时间，不是账号在 Confluence 的浏览历史。last_modified_at 取 version.when，保留源时区；必须为含时区的 ISO 8601 时间，缺失或格式错误明确失败，不用抓取时间替代。
- Cookie 字符串从忽略的本地 JSON 配置注入，只发送到配置 HTTPS 实例；禁止输出密钥或跨站重定向携带 Cookie。
- 与同一实例、同一树范围的最后成功 JSON 比较，输出新增、修改、删除节点 diff；修改包含版本、最后修改时间、标题、链接、父节点、顺序变化；最后查看时间变化不触发修改。首次导出明确标记 initial，无历史节点视为新增。
- 新快照和 diff 使用 schema_version 2，兼容版本 1 基线；旧节点未知时间在历史 diff 中明确为 null，首次补齐时间不单独触发修改，不伪造历史时间或改写历史文件。
- 每次生成独立快照与 diff，全部读取成功后才推进 latest 基线；鉴权、结构解析、请求重试仍失败时明确终止，不以残缺树覆盖基线。消失只代表当前凭据不可见，不证明物理删除。
- 新接缝仅用于独立 CLI 的 Confluence HTTP 读取，无新增依赖，不改变 Go Agent 工具运行时。

## 12. Confluence 批量 Markdown 导出技能

### 12.1 队列与目录同步

- `agents/skills/confluence-batch-export/` 读取页面树技能 output/latest.json，只对叶子页依次调用现有 url-to-markdown 技能，输出目录保留完整祖先层级；同级清理后重名用页面 ID 区分。
- 全量处理全部叶子；增量以最后完成批次的树为基线，使用页面树相同 diff 逻辑合并期间变化，首次使用最新配对 diff。父节点改名/移动影响后代叶子；消失或不再是叶子的已导出页面同步移除。增量不是缺失文件的全量修复，首次需要完整镜像时先全量。
- Markdown 中 confluence_page_id/confluence_version 为版本比较依据：相同跳过并计完成，树版本更大才替换；本地版本更大、缺少元信息、身份冲突明确失败。移动/改名即使同版本也同步路径。
- 新内容先由已有技能完整导出到暂存目录并核对页面身份、版本和资源，再删除旧页面目录、发布新目录。只删除本技能登记或通过页面身份校验认领的页面目录，不递归删除祖先目录或无关文件，拒绝符号链接与目录冲突。

### 12.2 进度与恢复

- 每项操作前后原子保存状态、已完成/总量、逐项结果和错误；进度命令只读。失败项保持未完成，默认再次运行先恢复冻结的上一批队列，不以新树覆盖未完成任务；最终目录校准完成后才允许建立新批次。
- 编码代理循环领取单项、调用 url-to-markdown、确认发布，直到全部完成；远程失败按现有技能重试规则终止并记录错误，保留旧内容和续跑点，不无限重试。脚本加进程锁防止并发修改状态。
- 仅新增 Python 标准库本地编排工具，复用既有导出接缝；不改变 Go Agent 运行时、认证、任务结构或数据模型，不新增外部依赖。当前 Go Agent 无 shell 工具，仍由有终端能力的编码代理执行。

### 12.3 完成后的目录校准

- 所有排队操作完成后，自动重新读取原树输入目录的 latest.json，与磁盘实际目录比较；latest 指页面树技能最后成功快照，不在此阶段自动重新抓取远程树。
- 根据最新树同步改名、移动、删除、叶子身份变化，补回已导出页面缺失的整个目录（正文或附件损坏明确报错）；期间新增/更新叶子进入补处理队列，完成后再次核对。相同版本仅调整本地目录，不重复下载。
- 全量要求全部最新叶子的 Markdown 齐全；增量保留原正文下载范围，历史未导出叶子只补齐空目录。树层级目录覆盖所有最新节点，assets 与 .batch 不参与页面树比较。
- 扫描磁盘实际页面位置，不只信任进度登记；目录标题和 Markdown 文件名均校准。无关文件、未知页面、重复页面 ID、符号链接或实例范围改变明确失败，保留现场与错误，禁止以完成状态掩盖差异。
- 进度区分 pending、verifying、complete；仅最终复核一致才标 complete，校准期间中断默认继续，不能跳过校准开始新批次。

## 13. Confluence 后台任务与浏览 **[已实现]**

### 13.1 两阶段异步任务

- 工作台 `/dashboard/confluence` 输入 Confluence 页面 URL，点击“生成目录树”立即建立任务并入 Asynq；阶段一复用 confluence-page-tree 的完整侧边栏抓取与校验，阶段一每任务使用最多 10 个并行节点采集（包含节点元数据和子目录请求），按原服务器顺序组装树；任一节点失败不得发布残缺快照，沿用原有实例凭据限制、30 秒请求超时与三次尝试。生成完整目录后停在 selection/waiting，展示目录供选择，不自动读取正文。阶段二选择一个节点及其子树中的叶子页，或全部叶子页，再由现有 URL 转换器串行导出 Markdown 与正文附件，目录保留完整祖先路径。
- 后台使用部署者配置的 HTTPS Confluence 实例及 Cookie；只允许输入属于该实例的页面 URL，不将部署凭据发给其他主机、不接受 URL 内凭据或跨站重定向。无需给 Go Agent 增加 shell 或宿主 MCP 工具。
- 新增 MySQL 导出任务表与 Asynq confluence:export 任务（载荷仅任务 ID 与用户 ID）。任务、树、Markdown 与附件均按登录用户隔离；API token 写操作沿用 can_write。
- 服务重启时将遗留 running/stopping 任务置 stopped；支持停止排队/运行任务与继续停止/失败任务；保存完整树及逐页检查点，继续时复用冻结树，已完成项不重复导出。远程版本与树不一致明确失败，不伪造版本；新任务重新抓树。新阶段二任务使用 markdown 阶段，选择范围在同用户同任务 RustFS selection.json 冻结，进度、文件读取和文件库均按所选计划计数；旧 export 阶段仍按原全量检查点续跑。选择与入队按 waiting/stopped/failed → selecting → queued 状态原子竞争；重启将 selecting 恢复 waiting，不改原始树 JSON、队列载荷或数据库结构。

- 阶段二完成后恢复节点选择、选中子树及全部导出按钮；再次提交复用原始冻结树建立同用户独立任务并自动切换到新任务，不重复采集目录、不修改上一批选择范围、完成量或已发布文件。停止或失败的未完成批次仍只允许按原检查点继续。新一批按本次所选范围导出，重选已导出页仍会再次读取；文件库继续汇集各批已发布内容。不改 Asynq 载荷、数据库结构或认证边界。

### 13.2 内容存储与进度

- 用户已明确选择引入 RustFS。树 JSON、Markdown、附件仅经内存保存到 RustFS，不写服务端本地磁盘；MySQL 仅保存任务元数据、阶段、完成量、总量与错误。既有独立 CLI 本地导出行为保持原契约。
- RustFS 官方 S3 SDK 接缝在 PutObject 明确返回 NoSuchBucket 时自动创建配置桶，再重试原对象写入；默认保持私有，不设置公共 ACL 或策略。并发创建返回 BucketAlreadyOwnedByYou 时继续写入；AccessDenied、BucketAlreadyExists、其他创建错误或重试仍失败必须明确返回，不能将权限错误或对象不存在当作缺桶。服务凭据需具备 CreateBucket 权限；配置缺失时功能明确返回不可用，不影响既有功能启动。
- Docker Compose 提供 rustfs 服务与独立命名卷 rustfsdata，密钥必须显式配置；API/控制台默认只映射本机 9000/9001 端口。server 依赖该服务启动，容器内端点固定为 http://rustfs:9000，本机 Go 使用 RUSTFS_ENDPOINT；桶不存在时由首次写入自动创建，不自动公开。
- docker-compose.dev.yml 同步提供相同 RustFS 服务与持久卷；make db-up / make dev 启动 MySQL、Redis、RustFS。开发 Go 进程运行在宿主机，沿用 .env 中的本机对象存储端点。
- 界面显示阶段、状态、完成量/总量、错误；提供手动刷新与每 10 秒自动刷新，离开页面清理定时器。
- 可选择最近任务查看完整树目录、已成功发布的 Markdown 和附件；未完成文件不可读，失败/停止保留已完成内容。所有读取经过同源鉴权代理，不公开桶或密钥，不渲染文档原始 HTML。
- Bun 与 Go 的内存 JSON 管道必须等待完整写入，大于 64 KiB 的 Markdown/附件文件包不得截断；规划和文件包解析失败需包含具体阶段、页面 ID 与响应字节数，不记录正文或附件内容。
- 经成功响应、页面身份与版本校验的合法空白页（空 HTML、空段落/换行或空白文本）正常导出元数据，标记 confluence_body_empty: true 并计入完成量；iframe 无法解析为正文时，在原位置保留 src 链接，协议相对和相对路径按实例 base 转为绝对地址，不递归读取嵌入页面。缺少 body.view、读取失败、无可保留链接的不可转换内容或非空正文转换丢失仍明确失败，不用空页掩盖。转换后正文为空但源页面包含未转换内容时，错误包含页面 ID 与完整页面 URL，便于定位原页面。
- Confluence 导出遇到 Jira 图标、外部 CDN、其他实例或当前实例非附件资源时，不尝试携带 Confluence 凭据下载，保留绝对 HTTP(S) 链接于 Markdown，不阻断正文导出。后台当前实例可识别附件下载成功则保存到 assets；404、其他 HTTP 错误、网络或读取异常在既有重试结束后记录告警并保留原始绝对链接，返回 HTML 同样保留链接，继续处理剩余附件和正文，不生成缺失附件的本地链接。全部附件不可用仍可发布正文；页面正文读取失败、页面身份或版本不匹配、转换数据非法、任务取消仍终止。非 HTTP(S) 资源（如 data:、file:）不进入附件下载计划，保留原引用，不读取本地文件或执行对应协议；HTTP(S) 资源 URL 含用户名或密码仍明确拒绝。保留远程或本地引用的文档不宣称完全离线。

## 14. Confluence 文件库与 ZIP 目录合并 **[已实现]**

### 14.1 多 Space 浏览与导出

- `confluence-library.json` 清单在导出与导入时统一遵循 64 MiB 单文件上限，不另设 2 MiB 限制；超过 2 MiB 的合法清单须可完整导出并重新导入，保留逐文件校验与其余 ZIP 限制。
- 独立模块 `/dashboard/confluence-library` 按当前用户汇集全部历史任务中已经发布的页面（包括未完成任务的已完成部分），不受任务列表最近 50 条限制；同实例、同 Space、同页面 ID 取 Confluence 最后修改时间最新者，Space 由实例地址与 Space key 区分。
- 显示多个 Space 根目录、层级目录、Markdown 与附件；文档和对象引用始终按用户隔离。可一键导出整库、Space 或选定子目录 ZIP，保留标题目录、Markdown、assets 相对链接与必要祖先目录。
- 文件库请求因内部或外部依赖错误返回 502 时，服务端必须记录结构化错误，包含 request_id、请求路径、Space、目录与具体原因，不记录鉴权头、Cookie 或正文；保留错误响应以便定位。
- ZIP 携带版本化 `confluence-library.json` 清单，包含实例、Space、页面 ID、版本、源页面最后修改时间及每文件路径、大小、SHA-256。导出逐文件从 RustFS 读取并流式压缩到响应，不在服务端或前端缓存整个 ZIP；导入流式上传到 RustFS 分块暂存，再以有界随机读取逐文件校验和确认发布，原文仅存 RustFS，不写服务端本地磁盘。下载 ZIP 上限 10 GiB、导出展开总量上限 50 GiB；单文件仍为 64 MiB、条目上限为 100000（含文件、目录和清单）。下载前使用相同导出链路校验，失败的具体 API 错误在页面显示，成功再交浏览器原生下载；校验与下载分别执行完整导出流程，增加对象读取和压缩耗时，下载开始后发生的断线或存储错误会产生不完整 ZIP 并记服务端日志。

### 14.2 导入检查与合并

- 用户明确选择仅导入本模块导出的 ZIP，不按普通文件的压缩时间猜测来源或覆盖顺序。导入先完整校验文件清单、大小与 SHA-256，再显示新增/更新/保留/冲突及逐页原因，确认后合并。
- 按 Confluence 页面最后修改时间比较：较新页面替换目录引用与内容，较旧页面保留现有；同时间且内容/版本/路径相同则保留，同时间不同内容保留现有并提示冲突。未涉及目录和页面保留，按实例/Space/页面 ID 合并，不凭标题覆盖不同页面。
- 导入使用私有不可变暂存对象，全部上传成功后整批可见；不删除旧任务结果。升级前未确认的旧单对象暂存预览需重新上传检查，已发布页面不受影响。确认前和发布前验证文件库摘要；目录已变化则拒绝过期预览，要求重新检查。新增 MySQL `confluence_library_imports` 保存按用户隔离的预览基线、元数据与发布状态，不保存文档正文；同用户确认操作使用数据库锁串行发布，API token 写操作沿用 can_write。
- 拒绝路径穿越、绝对路径、符号链接、重复文件/页面、清单外文件、缺少文件、损坏校验和及不支持格式。导入压缩包 ≤10 GiB、解压总量 ≤50 GiB、单文件 ≤64 MiB、条目 ≤100000（含文件、目录和清单），超限明确失败并提示选择较小目录；桶和已有权限不公开。
- 文件库依赖 MySQL 与 RustFS；仅实时 Confluence 抓取要求 Confluence Cookie。复用现有官方 S3 SDK，增加受限前缀对象列举；不新增外部依赖，不改变 Agent 工具运行时或既有 Asynq 任务载荷。

## 15. 定时爬虫与 Agent 报告 **[部分实现]**

### 15.1 任务与采集

- `/dashboard/crawl` 已提供任务管理、运行历史、报告查看和下载；代码与受控登录来源链路已实现，真实来源及真实模型验收尚未完成。用户在 dashboard 管理自己的爬虫任务，绑定当前用户可访问的现有 Agent；任务、运行记录与报告按用户隔离，API token 写操作沿用 can_write，不改变公共 Agent 只读规则。
- 复用 Asynq，每天 Asia/Shanghai 时区 09:00 触发，支持手动执行。同一任务排队或运行期间不重复启动，并记录跳过原因；服务重启后原 queued/running 轮次标记失败，旧消息不使终态复活，错过的定时不补跑，可手动重新执行。
- 任务 owner 可独立停用任务，即使引用的 Agent 已删除／停用或登录配置已撤权；仅停用不重新校验已失效的执行配置，修改其他配置或重新启用仍须完整校验。停用沿用 user_id 与 can_write 边界，不取消已领取轮次。
- 输入 1–20 个起始 HTTPS URL，跳数 hops 默认 0、范围 0–5：0 只抓起始页，大于 0 提取渲染后 DOM 的 a[href]，按 document.baseURI 解析相对地址，将 HTML 子页以剩余跳数减一加入单轮广度优先队列。允许跨域；图片与附件保留在线引用；支持 UTF-8 编码的 RSS 2.0 XML，其他非 HTML／RSS 内容跳过。可选 include_pattern 使用 Go RE2 正则匹配完整子链接（含查询参数），留空全部允许，不过滤起始页，非法正则保存时拒绝。去重限单次运行：移除 fragment、规范化域名和默认端口，保留路径、查询参数顺序及重复参数，不删除参数；同一规范 URL 只抓一次。单轮最多 100 个唯一候选页面（含起始页），超限明确失败并保留已采集对象，不静默截断。页面抓取与整份 Agent 报告并发由环境变量控制，默认分别为 3 和 1；具体来源与每轮规模尚未提供，真实站点适配和验收须使用后续确定的代表性页面。
- 支持需要登录的页面，通过 `internal/config` 加载环境变量 `CRAWL_PROFILES` JSON 数组；每项包含 id、精确 HTTPS origin、Cookie、获授权 user_ids 及登录成功且正文加载完成后可见的 ready_selector。配置修改后重启生效；不接收用户通过 API 提交原始凭据。部署者按域名配置 Cookie，任务可通过 profile_ids 引用多个当前用户获授权的登录配置，同一 origin 不可选多个配置；旧 profile_id 引用继续有效。未配置目标域名时匿名访问；配置数组为空时仍启用匿名任务的执行与每日调度。凭据不进入报告、Agent 输入、日志或普通 API 响应；登录失效记录页面异常并继续其他页面，由人工补充或更新 Cookie 后重新执行整轮，不实现自动登录、扫码或验证码处理。重定向仍不自动跟随，记录最终目标及状态码，提示检查登录／跳转配置；401、403、登录跳转及就绪元素未出现均保留判断依据，其中 403 及不明确的跳转只标记疑似登录或权限问题，不能断言缺 Cookie，也不能只凭 HTTP 200 判断已登录。
- 起始列表为 1–20 个 URL，每个 URL 最长 2048 个 UTF-8 字节；每页响应和渲染 HTML 上限 4 MiB，每次页面采集累计响应上限 16 MiB，正文上限 24000 个 Unicode 字符，浏览器输出协议上限 1 MiB。导航超时 30 秒，就绪元素等待 10 秒，单次浏览器进程上限 55 秒；单轮 Worker 上限 30 分钟。页面允许跨域独立采集，仅使用与目标精确 origin 匹配的配置和独立上下文；子资源仍仅允许本次目标 origin 的 GET/HEAD，不转发 Cookie 或 Authorization 到其他来源。阻止页面内跨域导航、自动跟随重定向、下载和 WebSocket。匿名页面以 body 就绪，配置登录来源继续使用 ready_selector。
- 登录配置 origin 使用规范 HTTPS 写法：ASCII 小写主机、省略默认 443 端口，IPv6 使用压缩地址；非规范配置在启动时明确拒绝。响应在接收及解压过程中执行单响应与共享累计大小限制，声明长度超限立即取消，不等待全量读取后才判定。

- URL 输入框接受网页和 RSS XML 地址，可混合填写。采集识别 application/rss+xml、application/xml、text/xml 及返回 XML 的 text/plain，不依赖 .xml 后缀；RSS 2.0 转换为一份完整 Markdown，包含订阅标题、描述、来源、更新时间、全部条目的标题、原文链接、发布时间、作者、分类与正文。条目标题与 description 至少提供一项；合法无标题条目保留编号和正文，只有标题的条目同样保留。正文优先使用 content:encoded，缺失时使用 RSS description；CDATA、XML 实体及正文 HTML 经已有转换器处理，图片仅保留远程引用，不执行正文脚本。
- XML 解析失败、真正的 DTD／实体声明、缺少 channel 或订阅标题等结构错误明确失败，不把 XML 源码当成 Markdown；CDATA、注释及处理指令内的声明字面内容不误判为 DTD。采集对象通过 format 标记 rss 或 html，兼容旧 HTML 对象缺少标记；RSS 单文档 Markdown 上限 240000 个 Unicode 字符，HTML 仍为 24000；沿用 4 MiB 响应、16 MiB 累计响应、1 MiB 进程协议和 55 秒进程限额，超限不截断。RSS 条目原文与正文超链接作为发现链接，沿用 hops、正则与单轮去重；hops=0 仍转换订阅内全部条目，不额外抓取原文。

### 15.2 报告与失败语义

- 采集完成后可通过 `/crawl-runs/{run_id}/markdown` 下载完整原文 Markdown（含全部 RSS 条目），按当前用户读取已捕获页面对象，合并下载上限 16 MiB。1 MiB 浏览器协议经 Go JSON 转义后保存的单页对象读取上限为 6 MiB，兼容既有对象；仍按格式校验解码后的正文字符上限，不因转义膨胀拒绝合法原文。该下载与 Agent 报告独立，Agent 失败时仍可下载；采集尚未完成时返回 409，失败或缺失对象明确报错。原文不经模型改写，不增加数据库字段或迁移。
- Worker 编排并发抓取，将正文交给现有 Agent 逐页提炼并汇总；每轮使用独立上下文，生成一份通用 Markdown 报告，包含总览、逐来源摘要、关键事实、来源链接与采集时间，保留历史报告，后台支持查看和下载。首版不要求特定主题或后续跟踪分析。
- Agent 每段最多读取 6000 个 Unicode 字符，全部分段进入提炼；摘要每组三项归并后生成报告。单次输入和系统提示词合计上限 24000 字符，模型单次回复上限 6000 字符；本地字符预算不声称精确匹配任意模型的 token 上限，模型上下文不足或返回非正常结束原因时明确失败。登录后的页面应排除无关导航；报告不执行原始 HTML 或加载远程图片。
- 网络、限流和临时服务错误最多重试 2 次，鉴权问题不重试，记录 URL、origin、最终目标、状态码和受控原因后继续，最终状态为 partial（部分完成），报告明确列出待补充 Cookie 的来源；全部页面均被登录拦截时也发布异常清单，不调用 Agent 加工空页面。其他页面最终失败立即取消同批及剩余采集，保留首个致命错误及页面失败记录，不以同批取消错误覆盖根因；本轮失败，不发布完整报告。Agent 加工或存储失败也不得标记成功，不以登录页、空回复或静默截断内容代替完整结果。
- 采集正文、中间内容与 Markdown 仅经内存处理或保存到 RustFS，不写服务端本地磁盘；MySQL 仅保存任务配置、运行元数据、进度、错误及对象索引。报告成功写入后才发布可见记录，不覆盖其他轮次的报告。

### 15.3 架构边界与交付

- 新增 Asynq `crawl:dispatch`（每日派发，不携带凭据或正文）与 `crawl:run`（载荷仅 user_id、运行 id）。每天九点由一个固定派发器读取当前启用任务，派发使用 crawl-control 独立队列与单路控制 Worker，避免长任务占满执行池后错过窗口；关闭时先停调度和控制队列，再收敛长任务。任务创建、编辑和停用无需单独注册调度项；只接受北京时间 09:00 这一分钟的派发，延迟到窗口外明确跳过。同一计划日期由 MySQL 唯一约束去重，任务记录行锁与在途唯一约束防止并发领取；排队记录每 30 秒检查派发，Asynq 按运行 ID 去重。运行历史接口返回每任务最近 100 轮，旧报告对象保留且仍按 owner 校验访问。
- `0005_crawl_tasks.sql` 创建 crawl_tasks、crawl_runs 和 crawl_pages；新增 `0006_crawl_recursion.sql` 扩展 profile_ids、hops、include_pattern，运行快照冻结这些配置，均只保存配置与元数据；不修改既有任务载荷与已应用迁移。具体结构及实施步骤见 [任务计划](PLAN_md/PLAN_261030.md)。
- 每 30 秒恢复检查同时核对 queued/running 记录与 Asynq 实际状态；消息已归档、已完成但数据库未终结，或运行消息丢失时，明确标记失败并释放占用，不重跑已开始的采集或模型调用。只有消息确实缺失的 queued 记录重新入队，不能将归档消息的 TaskID 冲突视为恢复成功。Compose 显式传递 CRAWL_PROFILES；Asynq 载荷仍仅携带用户与运行 ID。
- Agent 运行时边界扩展为后台报告调用，复用 langchaingo 接缝；本功能不依赖尚未实现的 MCP 工具执行或 subagent 调度，不改变交互实例重启后 stopped 的语义。
- 复用现有 RustFS S3 SDK 与 URL 转 Markdown 的浏览器和转换能力；浏览器无磁盘采集接缝须单独验证，不能直接把已有本地输出 CLI 当作无磁盘后台能力。Go、Bun 和前端未新增包依赖；Docker 运行环境改为 Debian，安装现有 Playwright 对应 Chromium 及系统依赖和 tzdata，构建使用单路 Go 编译。通用浏览器内存采集是新增 Worker 外部接缝，配置与限制按本节执行。

### 15.4 并发配置与运行测量

- 环境变量 WORKER_CONCURRENCY（默认 4）、CRAWL_CAPTURE_CONCURRENCY（默认 3）、CRAWL_TASK_CONCURRENCY（默认 3）、CRAWL_REPORT_CONCURRENCY（默认 1）分别控制共享 Asynq 执行池、全进程浏览器限额、单轮抓取批次、整份后台报告限额；均为 1–64 的整数，非法值启动失败，重启生效。独立 crawl-control 仍单路。上限均限单进程，多副本自行按总资源预算分配。整份报告内分段和归并继续顺序执行。
- 每个实际开始执行的轮次在既有私有 RustFS 用户／运行前缀保存 metrics.json，开始、阶段完成及终结更新快照，不覆盖其他轮次。保存版本、ID、状态、时间、排队等待、执行经过时间、当轮并发配置、各阶段与接缝累计耗时／次数／失败数、浏览器与模型限额等待、模型 HTTP 尝试及状态码、重试和退避、页面数与字符数。日志逐事件关联 run_id，禁止 URL 查询参数、Cookie、正文、提示词及服务错误响应体。
- 成功、partial、失败及正常取消都保存指标；取消后的终结记录使用独立有界上下文。指标存储失败显式返回错误并记录日志，不撤销已经发布的报告；硬杀进程只能保留最后成功快照，标记 running 的快照不可当作完整样本。尚未开始执行及被防重跳过的轮次沿用既有数据库记录，不伪造执行指标。指标保留随既有对象存储，不自动清理；无新数据库迁移、Asynq 载荷、包依赖或外部系统。
- 耗时以毫秒记录，阶段经过时间与并行接缝累计耗时不能相加解释为总执行时间。模型 HTTP 耗时仅到响应头，模型调用耗时包含响应体读取及重试。配置建议必须结合实际历史指标和宿主 CPU／内存观察；受控服务测试不替代真实站点与模型容量验收。

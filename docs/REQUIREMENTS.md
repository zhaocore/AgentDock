# dutybotGo 需求文档

本文件是仓库唯一权威需求文档（活文档）。行为变化或需求澄清后必须同步更新本文件；规则见 `AGENTS.md`。

层级约定：一级章节为模块/能力，二级小节为功能，列表项为验收细节。状态标注：**[已实现]**、**[部分实现]**、**[未实现]**。

> 本文档描述 dutybotGo 的目标形态（自 GraphGo 改造而来）。2026-10-16 改造主链路已完成：基础设施、仓库、Agent、实例、会话、前端与 swagger 均已实现，标注同步更新。

## 1. 产品定位

- dutybotGo：多用户 Go Agent 管理平台。部署者通过目录预设公共 Agent，所有用户共用 skills、MCP 与模型配置，各预设仅角色系统提示词不同；用户管理自己的实例和会话。
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
- Cookie 按精确主机匹配，显式点前缀域名允许子域；跨域资源和重定向不向无关域发送 Cookie。资源下载失败重试后明确报错，不生成声称完整的 Markdown。
- 技能独立依赖 Playwright、LinkeDOM、Turndown 与 GFM 插件，仅用于 CLI 浏览器/HTML 接缝，不改变 Go Agent 运行时或新增服务端执行工具。
- 输入 URL 含 `confluence` 时，编码代理先探测可读取该实例的 Confluence MCP；可用时读取 `body.view` 渲染 HTML 与版本，按正文引用通过 MCP 下载附件，再导出 Markdown。MCP 调用由编码代理编排，CLI 不硬编码 MCP 凭据或伪装可直接调用宿主工具。无匹配 MCP 时明确说明，再使用原有 Cookie 浏览器路径。
- Confluence 附件按正文引用的页面 ID、文件名定位，不依赖默认分页附件列表；原始二进制保存在标题目录的 assets/，重复资源去重，未下载或超出内联限制的资源明确失败，不静默漏图。

## 6. 后台 Worker **[已实现]**

- Asynq 任务类型：`repo:clone`、`repo:sync`、`repo:delete`。任务入队即返回 202，由 Worker 执行并回写状态。
- git 操作通过 `exec.Command` 调用系统 `git` 与 `codegraph` CLI；命令失败时错误信息（stderr 摘要）写入 `last_error`。

## 7. API 令牌 **[改造自已实现]**

- 沿用 `gk_` 前缀 API token：哈希存储、范围（scope）机制、`can_write` 写权限。
- 令牌可调用：仓库管理、Agent 管理、实例与消息、会话历史（自己的）等开放 API。
- 令牌禁止访问：`/auth/*`、`/api-tokens/*`、`/dashboard/summary`。
- 资源映射变化（原知识库 scope 改为仓库/Agent scope）在实现时同步更新 `api_token_scope`。

## 8. 管理后台（dashboard） **[已实现]**

- 项目名与界面全面改为 dutybotGo；移除知识库、文档、导入、图谱、查询等模块。
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

- 工作台 `/dashboard/confluence` 输入 Confluence 页面 URL，点击“开始任务”立即建立任务并入 Asynq；阶段一复用 confluence-page-tree 的完整侧边栏抓取与校验，阶段二复用 confluence-batch-export 的叶子及层级规划、现有 URL 转换器，串行导出 Markdown 与正文附件。
- 后台使用部署者配置的 HTTPS Confluence 实例及 Cookie；只允许输入属于该实例的页面 URL，不将部署凭据发给其他主机、不接受 URL 内凭据或跨站重定向。无需给 Go Agent 增加 shell 或宿主 MCP 工具。
- 新增 MySQL 导出任务表与 Asynq confluence:export 任务（载荷仅任务 ID 与用户 ID）。任务、树、Markdown 与附件均按登录用户隔离；API token 写操作沿用 can_write。
- 服务重启时将遗留 running/stopping 任务置 stopped；支持停止排队/运行任务与继续停止/失败任务；保存完整树及逐页检查点，继续时复用冻结树，已完成项不重复导出。远程版本与树不一致明确失败，不伪造版本；新任务重新抓树。

### 13.2 内容存储与进度

- 用户已明确选择引入 RustFS。树 JSON、Markdown、附件仅经内存保存到 RustFS，不写服务端本地磁盘；MySQL 仅保存任务元数据、阶段、完成量、总量与错误。既有独立 CLI 本地导出行为保持原契约。
- RustFS 官方 S3 SDK 接缝在 PutObject 明确返回 NoSuchBucket 时自动创建配置桶，再重试原对象写入；默认保持私有，不设置公共 ACL 或策略。并发创建返回 BucketAlreadyOwnedByYou 时继续写入；AccessDenied、BucketAlreadyExists、其他创建错误或重试仍失败必须明确返回，不能将权限错误或对象不存在当作缺桶。服务凭据需具备 CreateBucket 权限；配置缺失时功能明确返回不可用，不影响既有功能启动。
- Docker Compose 提供 rustfs 服务与独立命名卷 rustfsdata，密钥必须显式配置；API/控制台默认只映射本机 9000/9001 端口。server 依赖该服务启动，容器内端点固定为 http://rustfs:9000，本机 Go 使用 RUSTFS_ENDPOINT；桶不存在时由首次写入自动创建，不自动公开。
- docker-compose.dev.yml 同步提供相同 RustFS 服务与持久卷；make db-up / make dev 启动 MySQL、Redis、RustFS。开发 Go 进程运行在宿主机，沿用 .env 中的本机对象存储端点。
- 界面显示阶段、状态、完成量/总量、错误；提供手动刷新与每 10 秒自动刷新，离开页面清理定时器。
- 可选择最近任务查看完整树目录、已成功发布的 Markdown 和附件；未完成文件不可读，失败/停止保留已完成内容。所有读取经过同源鉴权代理，不公开桶或密钥，不渲染文档原始 HTML。
- Bun 与 Go 的内存 JSON 管道必须等待完整写入，大于 64 KiB 的 Markdown/附件文件包不得截断；规划和文件包解析失败需包含具体阶段、页面 ID 与响应字节数，不记录正文或附件内容。
- 经成功响应、页面身份与版本校验的合法空白页（空 HTML、空段落/换行或空白文本）正常导出元数据，标记 confluence_body_empty: true 并计入完成量；缺少 body.view、读取失败、仅含无法转换的嵌入内容或非空正文转换丢失仍明确失败，不用空页掩盖。
- Confluence 导出遇到 Jira 图标、外部 CDN、其他实例或当前实例非附件资源时，不尝试携带 Confluence 凭据下载，保留绝对 HTTP(S) 链接于 Markdown，不阻断正文导出；当前实例可识别附件仍下载到 assets，下载失败或字节校验失败仍报错。保留远程资源的文档不宣称完全离线。

## 14. Confluence 文件库与 ZIP 目录合并 **[已实现]**

### 14.1 多 Space 浏览与导出

- 独立模块 `/dashboard/confluence-library` 按当前用户汇集全部历史任务中已经发布的页面（包括未完成任务的已完成部分），不受任务列表最近 50 条限制；同实例、同 Space、同页面 ID 取 Confluence 最后修改时间最新者，Space 由实例地址与 Space key 区分。
- 显示多个 Space 根目录、层级目录、Markdown 与附件；文档和对象引用始终按用户隔离。可一键导出整库、Space 或选定子目录 ZIP，保留标题目录、Markdown、assets 相对链接与必要祖先目录。
- 文件库请求因内部或外部依赖错误返回 502 时，服务端必须记录结构化错误，包含 request_id、请求路径、Space、目录与具体原因，不记录鉴权头、Cookie 或正文；保留错误响应以便定位。
- ZIP 携带版本化 `confluence-library.json` 清单，包含实例、Space、页面 ID、版本、源页面最后修改时间及每文件路径、大小、SHA-256。导出逐文件从 RustFS 读取并流式压缩到响应，不在服务端或前端缓存整个 ZIP；导入流式上传到 RustFS 分块暂存，再以有界随机读取逐文件校验和确认发布，原文仅存 RustFS，不写服务端本地磁盘。下载 ZIP 上限 10 GiB、导出展开总量上限 50 GiB；单文件仍为 64 MiB、条目仍为 10000。下载前使用相同导出链路校验，失败的具体 API 错误在页面显示，成功再交浏览器原生下载；校验与下载分别执行完整导出流程，增加对象读取和压缩耗时，下载开始后发生的断线或存储错误会产生不完整 ZIP 并记服务端日志。

### 14.2 导入检查与合并

- 用户明确选择仅导入本模块导出的 ZIP，不按普通文件的压缩时间猜测来源或覆盖顺序。导入先完整校验文件清单、大小与 SHA-256，再显示新增/更新/保留/冲突及逐页原因，确认后合并。
- 按 Confluence 页面最后修改时间比较：较新页面替换目录引用与内容，较旧页面保留现有；同时间且内容/版本/路径相同则保留，同时间不同内容保留现有并提示冲突。未涉及目录和页面保留，按实例/Space/页面 ID 合并，不凭标题覆盖不同页面。
- 导入使用私有不可变暂存对象，全部上传成功后整批可见；不删除旧任务结果。升级前未确认的旧单对象暂存预览需重新上传检查，已发布页面不受影响。确认前和发布前验证文件库摘要；目录已变化则拒绝过期预览，要求重新检查。新增 MySQL `confluence_library_imports` 保存按用户隔离的预览基线、元数据与发布状态，不保存文档正文；同用户确认操作使用数据库锁串行发布，API token 写操作沿用 can_write。
- 拒绝路径穿越、绝对路径、符号链接、重复文件/页面、清单外文件、缺少文件、损坏校验和及不支持格式。导入压缩包 ≤10 GiB、解压总量 ≤50 GiB、单文件 ≤64 MiB、条目 ≤10000，超限明确失败并提示选择较小目录；桶和已有权限不公开。
- 文件库依赖 MySQL 与 RustFS；仅实时 Confluence 抓取要求 Confluence Cookie。复用现有官方 S3 SDK，增加受限前缀对象列举；不新增外部依赖，不改变 Agent 工具运行时或既有 Asynq 任务载荷。

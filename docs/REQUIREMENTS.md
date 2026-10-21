# dutybotGo 需求文档

本文件是仓库唯一权威需求文档（活文档）。行为变化或需求澄清后必须同步更新本文件；规则见 `AGENTS.md`。

层级约定：一级章节为模块/能力，二级小节为功能，列表项为验收细节。状态标注：**[已实现]**、**[部分实现]**、**[未实现]**。

> 本文档描述 dutybotGo 的目标形态（自 GraphGo 改造而来）。2026-10-16 改造主链路已完成：基础设施、仓库、Agent、实例、会话、前端与 swagger 均已实现，标注同步更新。

## 1. 产品定位

- dutybotGo：多用户 Go Agent 管理平台。部署者通过目录预设公共 Agent，所有用户共用 skills、MCP 与模型配置，各预设仅角色系统提示词不同；用户管理自己的实例和会话。
- 存储只依赖 MySQL + Redis：MySQL 存业务数据与会话历史，Redis 用于 Asynq 任务队列与缓存。不使用 PostgreSQL、Neo4j、RustFS、向量检索。
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

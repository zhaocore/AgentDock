# AGENTS.md — dutybotGo 编码代理操作手册

本文件是编码代理在本仓库工作的强制规则。所有规则适用于每一次改动，不分大小；不存在"小改动可跳过"的豁免。

## 从这里开始

每次改动前必读并按需更新：

1. 本文件
2. `docs/REQUIREMENTS.md`（缺失则先创建，再开始改动）
3. `docs/PLAN_md/` 下本次任务对应的计划文件（缺失则创建）
4. 架构参考：`docs/TECHNICAL_PLAN.md`、`README.md`

## 仓库地图

- `cmd/server/main.go` — HTTP 服务入口，API 与 Asynq 后台 Worker 同进程运行
- `cmd/migrate/main.go` — 迁移执行器（`make db-migrate`）
- `internal/api` — Gin 路由、handler、JWT 与 API 令牌组合鉴权中间件（transport 层）
- `internal/agent` — Agent 实例管理器与 langchaingo 运行时（外部 LLM 接缝）
- `internal/worker` — Asynq 任务：仓库克隆 / 同步 / 删除（git 与 codegraph CLI 接缝）
- `internal/store/mysql` — MySQL 业务数据存储（外部系统接缝）
- `internal/domain` — 领域模型
- `internal/config` — Viper 配置加载
- `migrations/` — 编号 SQL 迁移，服务启动时自动执行
- `docs/swagger` — swag 生成物，不手改

## 架构规则

- transport 逻辑（HTTP 参数解析、状态码、路由）只放 `internal/api`，业务规则不进 handler。
- Agent 运行时与实例生命周期只放 `internal/agent`，仓库任务只放 `internal/worker`，领域模型只放 `internal/domain`。
- 外部系统（MySQL、Redis/Asynq、LLM API、git / codegraph CLI）只能通过 `internal/store/*`、`internal/agent`、`internal/worker` 等既有接缝访问，业务代码不得直连。
- 配置一律走 `internal/config` + 环境变量，不在代码里写死连接串或密钥。

## 前端规则

`dashboard/` 使用 Next.js；复用组件在 `src/components/ui`，业务模块在 `src/features/dutybot`，API 类型与运行时校验在该模块的 `schemas.ts`。可复用状态、格式化与业务规则不进 DOM 胶水代码。

- 浏览器通过限定路径的 Next 同源代理访问 dutybotGo，JWT 只保存在 HttpOnly Cookie。
- 前端不直连数据库，不请求其他项目的 setup、refresh 或管理接口。
- 前端改动必须带 Vitest 交互测试与 Playwright 覆盖；使用稳定 test-id，禁用可见文本点击。
- 验证命令：`pnpm --dir dashboard typecheck`、`test`、`build`、`lint`、`test:e2e`。

## 领域守则

以下不变量不可漂移：

- 用户隔离：JWT 用户 ID 是私有 Agent、实例、会话、消息与 API 令牌的过滤范围。部署者维护的公共 Agent 定义（无用户 owner）全用户只读，用户只能创建、访问自己的实例与会话。公共定义不可经普通用户 API 修改或删除。API 令牌（`gk_` 前缀）鉴权后复用同一 `user_id` 上下文键，隔离同样生效。仓库是全局共享资源，不按用户过滤。
- API 令牌范围：范围为 `all`；写操作额外要求 `can_write`。令牌禁止访问 `/auth/*` 与 `/api-tokens/*`。
- 会话历史：会话按用户软删除；列表、消息读取与删除均按 `user_id` 过滤。
- Agent 实例：进程内长驻 goroutine，服务重启后全部置 `stopped`，会话历史以 MySQL 为准。
- 仓库任务：clone / sync / delete 由 Asynq 编排；删除本地目录只允许 `REPO_ROOT_DIR` 子路径；codegraph 初始化失败不阻断仓库 `ready`。
- 已应用的 migrations 不可修改，只能新增编号文件。

## 测试策略

- 每次后端逻辑改动必须同时写或改单测；每次改动结束前必须跑完整单测：

  ```bash
  make test        # 等价于 go test ./internal/... -count=1
  ```

- **覆盖率不回退**（Go 后端强制规则）：改动后端逻辑代码后，单测语句覆盖率不得低于改动前。流程：

  ```bash
  go test ./internal/... -coverprofile=coverage.out -count=1
  go tool cover -func=coverage.out | tail -1   # 查看 total
  ```

  当前实测基线：**61.6%**（2026-10-16，dutybotGo 改造后重建基线）。改动前记录 total，改动后 total 必须 ≥ 改动前；低了就补测试直到达标。`coverage.out` 为忽略的本地生成物，改动后更新它以反映最新基线。
- 集成测试命令：`make test-integration`（`-tags=integration`）；既有测试覆盖真实存储、发布、删除、重建与工作台 SQL。运行前按测试要求显式配置独立测试依赖；跳过用例不等于完成验收。
- `internal/domain`、`internal/store/mysql`、`internal/agent`、`internal/worker` 已有测试；触碰这些包时同步验证契约。
- 测试中的外部系统用假实现或 mock（go-sqlmock、httptest），不依赖真实数据库。
- 模糊不清的测试失败必须向用户报告，不得静默跳过。

## 文档规则

- `docs/REQUIREMENTS.md` 是唯一权威需求文档，缺失时必须先创建。
- 需求按最多三层层级组织：一级为模块/能力，二级为功能，三级为验收细节。
- 需求是活文档：行为变化或被澄清后必须同步更新。

## 计划规则

- 每个任务按时间顺序以 checklist 形式记录在 `docs/PLAN_md/PLAN_yymmdd.md`（yymmdd 为任务开始日期），文件不存在就创建。
- 完成一项勾一项，不留未勾但实际已完成的项。

## 一致性检查

- 每次改动前对照 `docs/REQUIREMENTS.md` 当前内容；改动与需求冲突或需求含糊时，向用户提出，不得猜着做。

## 反馈回路

- 用户测试或反馈暴露行为不符时，同时更新 `docs/REQUIREMENTS.md` 与相关测试，让期望行为随时间更精确。

## 安全与配置卫生

- `.env` 含真实密钥，已 gitignore；绝不提交，绝不把值写进代码、日志或文档。
- `JWT_SECRET`、`LLM_API_KEY` 等敏感项只从环境变量读取。
- 日志不得打印文档全文或密钥；原始文档仅存 RustFS，不写本地磁盘。

## 禁止承载业务逻辑的路径

`coverage.out`、`bin/`、根目录二进制、`docs/swagger/`、`migrations/` 已应用文件、`.env`、`repo/`（仓库克隆目录）、`.codegraph/`、`.cursor/`。

## 变更审批

以下改动必须写入 `docs/REQUIREMENTS.md` 对应条目、计划文件和 commit message，留下明确说明：

- Asynq 任务结构变化
- 数据模型变化：MySQL 表结构、migrations 新增
- 用户隔离或认证机制变化（含 API 令牌范围与权限边界变化）
- Agent 运行时边界变化（langchaingo 调用方式、MCP / subagent 接入方式）
- 新增外部依赖或新的外部系统接缝

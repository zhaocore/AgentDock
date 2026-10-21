# dutybotGo

> 多用户 Go Agent 管理平台：基于 langchaingo 的 Agent 服务 + 仓库托管（git clone 与 codegraph 索引初始化）。

dutybotGo 由部署者维护公共 Agent 预设、skills 与 MCP 配置，用户启动自己的实例并与它会话；添加的 Git 仓库由后台 Worker 克隆到指定目录并初始化 codegraph 索引。存储只依赖 MySQL + Redis（Asynq）。

**当前状态：改造完成初期。** 认证、仓库管理、Agent/实例/会话主链路可用，MCP 工具桥接与 subagent 工具化调用为提示词注入实现，后续版本接入 langchaingo agent 循环。

## 核心能力

- **仓库管理**：全局共享的 Git 仓库登记；后台任务 `git clone` / `fetch + reset` 到 `REPO_ROOT_DIR`（默认 `./repo`），`.codegraph` 不存在时触发 `codegraph init -i`。
- **Agent 定义**：公共预设全用户只读、共享技能与模型，各角色仅提示词不同；原有私有 Agent 与历史保留。
- **Agent 实例**：服务进程内长驻 goroutine，支持多实例、启停；重启后实例置 stopped 可再启动。
- **会话历史**：按用户隔离持久化到 MySQL，支持发消息（服务端调用 langchaingo 执行）、分页列表、软删除。
- **认证**：JWT + bcrypt；`gk_` 前缀 API 令牌（范围 all + can_write），可调用开放 API。
- **管理后台**：Next.js（`dashboard/`），同源代理 + HttpOnly Cookie，仓库 / Agent / 会话 / API 密钥管理。

## 技术栈

| 模块 | 实现 |
| --- | --- |
| HTTP API | Gin |
| 身份认证 | JWT + bcrypt + API 令牌 |
| Agent 运行时 | langchaingo（OpenAI 兼容端点）|
| 异步任务 | Asynq + Redis |
| 业务数据 | MySQL 8 |

## 快速开始

```bash
make db-up      # MySQL + Redis
cp .env.example .env   # 填 JWT_SECRET / LLM_API_KEY / LLM_BASE_URL
make run        # 启动服务（自动迁移）
```

管理后台：`pnpm --dir dashboard dev`。

## 环境变量

| 变量 | 说明 | 默认 |
| --- | --- | --- |
| `MYSQL_HOST/PORT/USER/PASSWORD/DB` | MySQL 连接（或 `DATABASE_URL` DSN）| localhost:3306 |
| `REDIS_HOST/PORT/PASSWORD` | Redis（或 `REDIS_URL`）| localhost:6379 |
| `REPO_ROOT_DIR` | 仓库克隆根目录 | `repo` |
| `AGENT_CONFIG_DIR` | 部署者维护的公共 Agent 配置目录 | `agents` |
| `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` | OpenAI 兼容 LLM 配置 | — |
| `JWT_SECRET` | JWT 签名密钥 | 必填 |

公共预设入口为 `agents/presets/agent1.md`、`agent2.md`；公共技能放入 `agents/skills/<name>/SKILL.md`，MCP 配置放入 `agents/mcp.json`。启动或管理页刷新时同步，下一轮消息使用新提示词；MCP 实际工具调用尚未接入。配置格式、停用与权限规则见 [需求文档](docs/REQUIREMENTS.md#55-公共配置与预设-已实现)。Docker 默认只读挂载 `./agents`，自定义目录时同步调整挂载。

## 常用命令

```bash
make test        # 单元测试
make swagger     # 重新生成 Swagger（/swagger/index.html）
make db-migrate  # 独立执行迁移
```

# dutybotGo

> 多用户 Go Agent 管理平台：基于 langchaingo 的 Agent 服务 + 仓库托管（git clone 与 codegraph 索引初始化）。

dutybotGo 由部署者维护公共 Agent 预设、skills 与 MCP 配置，用户启动自己的实例并与它会话；添加的 Git 仓库由后台 Worker 克隆到指定目录并初始化 codegraph 索引。业务存储依赖 MySQL + Redis（Asynq）；Confluence 文档使用 RustFS 私有对象存储。

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
cp .env.example .env   # 填 JWT_SECRET / LLM_API_KEY / LLM_BASE_URL 与 RUSTFS_ACCESS_KEY / RUSTFS_SECRET_KEY
make db-up      # MySQL + Redis + RustFS
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

## Confluence 后台导出

管理页 `/dashboard/confluence` 输入部署者配置实例中的页面地址。点击“生成目录树”只运行阶段一，阶段一最多 10 个节点并行采集，生成完整左侧目录后等待选择。选中某个节点并点击“导出选中子树”，或点击“全部导出 Markdown”，才开始阶段二；沿用叶子页导出规则并保留完整祖先目录。支持停止、继续、完成量/总量、手动刷新和每 10 秒自动刷新。选择最近任务查看目录及已发布的 Markdown 和附件。重启后原运行任务变为已停止，可从已保存检查点继续；开始新任务重新抓取树。选定范围会冻结，停止或失败后继续不会扩大范围；旧任务按原全量计划续跑。

启用时配置 `.env.example` 中全部 `CONFLUENCE_*`、`RUSTFS_*` 项。Cookie 由部署者通过环境变量提供，须有对应空间读取权限；浏览器只提交属于 `CONFLUENCE_BASE_URL` 的 HTTPS 地址。RustFS 使用私有桶；首次写入返回 `NoSuchBucket` 时自动建桶并重试，服务账号需具备 `CreateBucket` 和对象读写权限；`RUSTFS_ENDPOINT` 使用服务可访问的 S3 API 地址。凭据不得提交。

Compose 包含 `rustfs` 服务和持久卷 `rustfsdata`。先填写 `RUSTFS_ACCESS_KEY`、`RUSTFS_SECRET_KEY`，运行 `docker compose up -d rustfs`；默认桶名为 `dutybot-confluence`（由 `RUSTFS_BUCKET` 配置），首次写入时自动创建，也可在本机控制台 `http://localhost:9001` 查看。默认 S3 API 为 `http://localhost:9000`，两个端口只绑定本机；`server` 容器自动使用 `http://rustfs:9000`。修改宿主端口用 `RUSTFS_PORT` / `RUSTFS_CONSOLE_PORT`，本机 Go 的 `RUSTFS_ENDPOINT` 同步调整。Compose 依赖条件只保证容器启动，首次使用前需确认服务可访问；建桶权限不足或桶名被其他账号占用会明确报错，不自动公开桶或修改已有权限。

开发配置 `docker-compose.dev.yml` 提供相同的 MySQL、Redis、RustFS 基础设施；`make db-up` 和 `make dev` 均包含 RustFS。开发模式下 Go 服务在宿主机运行，使用 `.env` 的 `RUSTFS_ENDPOINT=http://localhost:9000`。

服务通过内存管道运行已有技能采集和转换函数；文档、附件及完整树仅存 RustFS，任务进度存 MySQL。独立 CLI 仍使用原本地输出。原生部署需 Python 3.10+、Bun 和 `bun install --cwd agents/skills/url-to-markdown --frozen-lockfile`；Docker 镜像包含依赖。若宿主挂载 agents，请保留 compose 的依赖卷。未配置导出服务时，其他功能照常运行，导出 API 返回 503。


## 定时采集报告

入口 `/dashboard/crawl`，配置和验收边界见 [需求第 15 节](docs/REQUIREMENTS.md#15-定时爬虫与-agent-报告-部分实现)。原生部署在安装技能依赖后，还须运行 `bun agents/skills/url-to-markdown/node_modules/playwright/cli.js install chromium` 安装对应浏览器；Linux 同时安装浏览器系统依赖（`install --with-deps chromium`）。Docker 镜像已包含 Debian、时区数据与对应 Chromium；建议使用 Docker Buildx/BuildKit 构建（`docker buildx build --load -t dutybot:local .`），当前旧版 legacy 引擎存在快照错误。登录配置使用 `.env.example` 的 `CRAWL_PROFILES`，正文就绪元素由部署者按真实站点确定；配置修改后重启服务。

并发环境变量、逐轮持久指标、日志提取和瓶颈判断见 [采集性能说明](docs/CRAWL_PERFORMANCE.md)。默认保持 Worker 4、全局／单轮抓取 3、整份报告 1；重启生效。

## Confluence 文件库

打开 `/dashboard/confluence-library`，按 Space 查看历史任务已导出的目录、Markdown 与附件。可下载整库 ZIP，或选择 Space/子目录后导出；未完成任务中已成功导出的页面也会汇入文件库。

导入仅接受本模块导出的 ZIP（含 `confluence-library.json` 清单），先显示新增、更新、保留与冲突，再确认合并。相同实例、Space、页面 ID 按 Confluence 最后修改时间保留最新；同时间内容不同保留现有并提示冲突。其他目录和原任务结果保留；预览后目录发生变化时需重新导入检查。

下载 ZIP 最多 10 GiB，导出展开总量最多 50 GiB，逐文件流式压缩，不缓存整个压缩包。页面先执行完整导出检查，API 具体错误直接显示；检查成功再由浏览器原生下载，因此检查与下载分别执行完整导出流程，会增加对象读取与压缩耗时。传输开始后的断线或存储失败会留下不完整 ZIP 并记录服务端错误。导入同样为 ZIP 最多 10 GiB、解压最多 50 GiB，经流式代理写 RustFS 分块暂存，再逐文件校验与发布，不整包缓冲、不落本地磁盘。升级前尚未确认的旧 ZIP 预览需重新上传检查；导入和导出均限制单文件 64 MiB、最多 100000 个条目（含文件、目录和清单），超限请分目录。更新后启动后端会自动执行 `0004_confluence_library.sql`，文件库仅需 MySQL 与 RustFS，实时抓取才需要 Confluence Cookie。完整规则见 [需求文档第 14 节](docs/REQUIREMENTS.md#14-confluence-文件库与-zip-目录合并-已实现)。

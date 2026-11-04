# 采集并发与瓶颈分析

## 配置

配置入口为 `.env` 或进程环境变量；Compose 已显式转发。修改后重启服务。默认值保留现有行为，每项接受整数 1–64；非法非空值启动失败，空环境项沿用 Viper 的未配置语义。

| 环境变量 | 默认 | 限制范围 |
| --- | --- | --- |
| `WORKER_CONCURRENCY` | 4 | 单进程 Asynq default 执行池；爬虫、仓库、Confluence 共享 |
| `CRAWL_CAPTURE_CONCURRENCY` | 3 | 全进程所有轮次共用的浏览器限额；HTML 和 RSS 共用 |
| `CRAWL_TASK_CONCURRENCY` | 3 | 单轮采集批次大小；实际仍受全局浏览器限额约束 |
| `CRAWL_REPORT_CONCURRENCY` | 1 | 全进程同时生成的完整报告数；不影响交互会话 |

单任务按批次等待最慢页面，然后落盘并发现下一批；报告内各分段、各级归并仍串行。`crawl-control` 固定独立单路。多副本上限会累加，MySQL 连接池、Chromium 内存和模型 RPM/TPM 仍需共同预算。

可试验的下一档配置：

```dotenv
WORKER_CONCURRENCY=8
CRAWL_CAPTURE_CONCURRENCY=3
CRAWL_TASK_CONCURRENCY=3
CRAWL_REPORT_CONCURRENCY=2
```

这组值尚未通过真实站点／模型负载验收。先对同等页数、字符量与模型的任务比较耗时、429、重试、宿主内存；每次只调整一个限额。启动日志 `worker concurrency configured` 可核对实际生效值。

## 每轮积累的数据

每个开始执行的运行在私有桶保存 `crawl/<user_id>/<run_id>/metrics.json`。开始、采集完成、报告完成及终结写快照；各轮独立保留。终态包括 `succeeded`、`partial`、`failed`、`canceled`；`canceled` 对应数据库已有的 failed 终态。旧运行不补造指标，防重跳过和尚未执行的排队记录继续保留在 MySQL。

指标写入失败显式返回错误并记日志；已发布的报告不撤销。正常取消使用独立的 10 秒上下文保存终结指标。进程硬杀、主机故障只能保留最后成功快照，`running` 不可纳入完整耗时样本。指标随 RustFS 卷保留，无自动删除策略；运维须按容量管理生命周期。

| 字段／计时项 | 含义 |
| --- | --- |
| `version`、运行／用户／任务 ID、时间、`concurrency` | 版本、关联标识、实际限额；不含来源 URL 或凭据 |
| `queue_ms` | MySQL 运行创建到 Worker 领取完成、建立测量上下文；含投递恢复等待 |
| `elapsed_ms` | 本轮建立测量上下文到快照时刻的实际经过时间 |
| `capture_phase` / `report_phase` / `publish_phase` | 阶段实际经过时间；采集含浏览器等待、逐页数据库写入和对象上传；报告含限额等待与模型处理 |
| `capture_wait` / `report_wait` | 全局浏览器／整份报告限额等待；正常竞争通常有极小非零值 |
| `page_capture` | 每页采集调用，含限额等待与重试；并行累计 |
| `browser_attempt` / `browser_backoff` | 单次浏览器进程／重试退避累计耗时 |
| `model_call` | 一次完整 Runtime.Execute，含请求重试和响应体读取；不含后续本地输出校验 |
| `model_http_headers` / `model_backoff` | HTTP 尝试到响应头／重试退避；响应体读取耗时计入 model_call |
| `database_read` / `database_write` | 执行阶段数据库调用，含连接等待；不含领取前查询，也不细分 SQL 或连接池等待 |
| `object_put` | 业务对象上传，含 SDK 内部重试；不包含指标对象上传 |
| `metrics_put` | 指标上传；快照只包含此前上传，当前快照自身上传耗时见 measurement 日志 |
| `counters` | 页面分类数、已保存正文字符数、HTML/RSS 数、浏览器／模型重试数、浏览器错误 HTTP 状态与模型 HTTP 状态次数；模型状态 0 表示未获得响应 |

所有计时单位为毫秒；`timings` 每项包含 count、failures、total_ms、max_ms。`total_ms` 是累计值，**并行操作与外层阶段不可相加当作总耗时**。页面计数是已处理分类结果；前面的失败导致提前终止时，同批剩余页面可能已请求，但未分类入库，请结合 page_capture.count。模型 token 用量、浏览器子资源请求数与 SDK 内部重试次数未统计，不将字符数当作 token 数。

`slog` 在服务中输出 JSON；`crawl measurement` 逐操作记录 run_id、耗时与失败标记，`crawl metrics snapshot` 记录完整安全指标，重试告警也带 run_id。不记录正文、提示词、Cookie、URL 查询参数或模型错误响应体。第三方 Asynq/Gin 日志保留自身格式。Compose server 使用 local 日志驱动，单文件 20MB、最多 5 份轮转；容器删除会丢日志，长期分析以 RustFS 指标为准。

## 提取并汇总

使用部署者已有的 S3 权限及 AWS CLI，仅下载指标对象。以下命令要求凭据已在当前环境中，禁止将实际密钥写入脚本或提交：

```bash
# 指定要分析的用户；避免下载正文或其他对象。
export CRAWL_USER_ID='<user-id>'
AWS_ACCESS_KEY_ID="$RUSTFS_ACCESS_KEY" AWS_SECRET_ACCESS_KEY="$RUSTFS_SECRET_KEY" AWS_DEFAULT_REGION="$RUSTFS_REGION" \
  aws --endpoint-url "$RUSTFS_ENDPOINT" s3 cp \
  "s3://$RUSTFS_BUCKET/crawl/$CRAWL_USER_ID/" /tmp/agentdock-metrics/ \
  --recursive --exclude '*' --include '*/metrics.json'

# 按配置及结果状态分组，避免把失败、partial 与完整成功混在一起。
find /tmp/agentdock-metrics -name metrics.json -exec jq -c . {} + | jq -s '
  map(select(.version == 1 and .status != "running")) |
  group_by([.concurrency, .status]) | map({
    concurrency: .[0].concurrency, status: .[0].status, runs: length,
    avg_queue_ms: (map(.queue_ms) | add / length),
    avg_elapsed_ms: (map(.elapsed_ms) | add / length),
    p95_elapsed_ms: (map(.elapsed_ms) | sort | .[(length * 0.95 | ceil) - 1]),
    avg_capture_ms: (map(.timings.capture_phase.total_ms // 0) | add / length),
    avg_capture_wait_ms: (map(.timings.capture_wait.total_ms // 0) | add / length),
    avg_report_wait_ms: (map(.timings.report_wait.total_ms // 0) | add / length),
    avg_model_ms: (map(.timings.model_call.total_ms // 0) | add / length),
    avg_storage_ms: (map(.timings.object_put.total_ms // 0) | add / length),
    avg_database_ms: (map((.timings.database_read.total_ms // 0) + (.timings.database_write.total_ms // 0)) | add / length),
    model_retries: (map(.counters.model_retries // 0) | add),
    rate_limited_requests: (map(.counters.model_http_status_429 // 0) | add)
  })'

docker compose logs --since 24h server
# 结合容器 CPU／内存，判断浏览器是否受宿主资源限制。
docker stats --no-stream
```

目录每次会同步最新快照；需排除已经硬杀且仍为 running 的样本。按相近页数、字符量、RSS/HTML、Agent/模型另选样本比较；当前指标不记录模型名称，须按 task_id 关联冻结的 agent.json，勿将其中提示词输出到分析日志。

## 当前瓶颈结论与证据

源码确认：报告整份持有限额，各分段和归并串行；等待报告限额仍占 Asynq 执行位。默认单路报告可能让多个 Worker 等待，阻碍后续轮次采集。RSS 全文增长会增加分段及归并调用次数。抓取按批次推进也存在慢页面拖住同批次的问题。

2026-10-30 本机受控结果（非生产容量结论）：

| 场景 | 限额变化 | 总经过时间 | 累计接缝时间 | 观察 |
| --- | --- | --- | --- | --- |
| 4 份报告，每份 2 次 HTTP；每次服务端等待 25ms | 报告 1 → 2 | 217.4 → 115.7ms | model_call 216.4 → 230.8ms | 累计报告等待 327.4 → 114.3ms；最大 HTTP 并发 1 → 2 |
| 6 页，每页受控 Source 等待 20ms | 单轮 1 → 3 | 126.8 → 42.5ms | page_capture 125.1 → 126.1ms | 最大采集并发 1 → 3；数据库使用测试替身，无浏览器负载 |

报告验证使用真实本地 HTTP 与生产 Reporter／重试接缝；页面实验只验证编排限额。重试用例还验证了 503 两次失败后成功、两次退避均被统计，限额饱和取消保留等待指标。这些结果说明指标能区分工作耗时与限额竞争，不能据此断言生产模型、网络或数据库的当前占比。

独立 MySQL、Redis、RustFS 与真实 Chromium 的 RSS 端到端样本：28111 字符，执行约 691ms，其中浏览器尝试约 647ms，9 次本地受控模型调用累计约 3.9ms，业务对象上传累计约 13.6ms。该样本的采集阶段占主导；本地模型未模拟真实生成速度，因此不能迁移为生产结论。

后续按历史数据判断：

| 观察 | 下一步 |
| --- | --- |
| report_wait 高、模型 429 少、模型延迟稳定 | 先试报告 1 → 2；同时观察队列等待是否下降 |
| model_call 高、report_wait 低 | 模型请求本身／分段数量占主导；增加 Worker 不会加速同份报告 |
| model_retries、429、model_backoff 高 | 模型限流或暂时故障；提高报告并发可能恶化 |
| capture_wait 高且主机内存充裕 | 评估全局抓取限额；单任务限额超过全局限额不会增加浏览器吞吐 |
| browser_attempt 高、capture_wait 低 | 检查实际来源网络、页面就绪条件及 Chromium CPU／内存；现有进程指标不能再细分导航和 DOM 转换 |
| queue_ms 高、report_wait/capture_wait 低 | 结合共享仓库／Confluence 任务判断执行池是否不足；再试 Worker 4 → 8 |
| object_put 或 database_write 高 | 优先定位 RustFS／MySQL 与连接等待，保留 SDK 重试和网络检查边界 |

真实来源、真实模型配额及部署主机资源尚未采样；当前无法给出可信的生产瓶颈排名。先积累代表性多轮数据，再据上述字段定位。

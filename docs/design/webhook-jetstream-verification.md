# Webhook JetStream 验证记录

## 并发配置与消息回调 panic 隔离（2026-09-30）

在当前未提交的 Consume 实现上补充 `webhook.concurrency`，默认 10，显式零和负数启动校验失败。每实例按该值建立会话，专用 Transport 的总空闲、单 Host 空闲和连接上限同步变化；修改配置需重启实例，不改变共享 Consumer 的限制。

消息处理回调在当前 goroutine 中 recover，错误日志只记录处理位置和堆栈，不记录 panic 原始值。恢复后同一会话继续处理，未确认消息按 AckWait/MaxDeliver 恢复，不新增重试队列或接收端失败统计。发送后的异常仍可能造成重复；此边界不保护其他 goroutine 或不可恢复的运行时故障。

- `TestWebhookConcurrencyConfiguration` 覆盖默认、自定义及零/负数拒绝。
- `TestWebhookConsumeConfiguredConcurrency` 使用真实文件存储 JetStream，覆盖内部默认、1、4、12 个位置，验证会话数、实际处理上限、HTTP 上限和取消后无统计写入。
- `TestWebhookConsumePanicRecoveryAndRedelivery` 仅启动一个位置，模拟首次查询 panic，验证继续处理下一条、原消息按 AckWait 重投、无接收端失败计数及结构化日志不包含模拟凭据。
- 配置和 Webhook 包完整 race 检测通过（1.676s、12.522s），日志 `/tmp/webhook-concurrency-panic-race.log`。

- 最新 `just test` 使用本轮专用数据库执行，配置及 Webhook 包通过（3.394s、16.467s），全部 Webhook 资源和投递用例未失败；全量未通过：`TestSandboxLifecycleDurableScheduleDispatchesReclaim` 调度超时、`TestSessionPendingToolRulesMatchAcceptance/child` 返回 409 而非 200。日志 `/tmp/webhook-safety-just-test.log`。没有修改这些模块或跳过失败项。
- 调度用例随后在复用库和另一全新专用库定向复跑均通过（2.540s、2.296s），日志 `/tmp/webhook-safety-schedule-{reused,fresh}.log`；全量并发测试中的超时仍保留为未解决结果。Session 子线程的未修改 HEAD 复现证据见下文上轮记录，未把定向通过表述为全量通过。
- `just lint`、`just dead-code`、`just duplicates`、`just complexity`、`just large-files`、`just hooks-run` 均通过，三个未跟踪文件另行显式 hooks 通过；日志 `/tmp/webhook-safety-{lint,dead-code,duplicates,complexity,large-files,hooks-run}.log` 和 `/tmp/webhook-safety-new-files-hooks.log`。本轮没有前端改动，未扩大前端功能测试。

两个本轮专用 PostgreSQL 测试库及专用对象存储桶已清理；嵌入式 NATS 和接收器随测试结束停止，开发数据库、开发桶和用户已有容器未改动。代码没有暂存或提交。

Review 检查了配置默认值与拒绝语义、会话与连接上限、recover 只覆盖消息回调、恢复后的补充消费、未确认消息保留、日志不暴露 panic 值以及取消清理。未发现需要额外修复的逻辑问题；已写统计和 HTTP 发送不能因 panic 撤回，仍需接收端去重。

本轮验收不包括 Console 人工操作、长期容量压测或所有不可恢复故障。下文保留前一次 Consume 实现的验证记录。


## Consume 持续消费验证（2026-09-30）

基于 `fd87782`，分支 `codex/webhook-subscriptions`。每实例 10 个 Consume 会话，共享原 durable Consumer 和连接，各会话显式 PullMaxMessages(1)、同步处理，完成即可补充。无新增 schema、队列、公开 API 或前端行为；发布、有限消费机会、重试、失败窗口和签名不变。

| 范围 | 当前测试证据 |
| --- | --- |
| 自动补充、单实例最多 10 条、HTTP 期间无数据库连接占用 | `TestWebhookWorkerAutomaticRefill`：阻塞前 10 条，仅释放其中一条，第 11 条在其余九条仍阻塞时进入 |
| 两实例共享消费 | `TestWebhookWorkerMultipleInstances`：首实例占满 10 个位置，次实例再处理另外 10 条；检查事件 ID 无重复 |
| 初始化部分失败、回调退出、无遗留订阅 | `TestWebhookConsumeStartupFailureCleanup`：第 4 个会话初始化失败，前三个实际进入处理，再验证取消、关闭通知、连接订阅数恢复、无统计写入 |
| 单条期限、较长 HTTP 超时、并发重复停止 | `TestWebhookConsumeTimeoutAndConcurrentStop`：T=10s/70s，检查 T+15s 与 AckWait，10 条在途并发调用 stop |
| 10 条真实 HTTP 在途停止 | `TestWebhookWorkerStopCancelsActiveRequests`：请求全部退出，消息留待恢复，失败窗口不变 |
| 意外关闭会话恢复、空闲停止、永久连接关闭 | `TestWebhookConsumeSessionRestartsAndIdleStop`、`TestWebhookConsumePermanentConnectionClosure` |
| 文件存储 JetStream 重启后恢复 | `TestWebhookConsumeNATSRestart`：重启同端口、同存储目录的 NATS，原 Worker 和连接恢复消费 |
| 跨消息复用 HTTP 连接、停机关闭 | `TestWebhookConsumeReusesAndClosesHTTPConnection` |
| 重试、目标隔离、禁用、当前配置、统计故障及签名 | 原投递测试改为真实 Worker.Start；`TestWebhookRetryContinuousConsumer`、`TestSessionWebhooks`、失败窗口及资源入口测试仍覆盖成功和失败边界 |

所有 Webhook 投递测试已移除 RunOnce，改为启动 Consume 并等待队列、订阅和接收状态；资源生产失败、重复和级联断言保留。本轮没有扩展此前待办的全事件独立端到端回归体系，也没有执行 Console 人工验收或长期内存/容量压测。

### 最新执行结果

- 使用专用 PostgreSQL 数据库、专用测试桶、逐测试的文件存储 JetStream 和本地接收器；未使用开发数据库或付费 sandbox。
- 定向 race：`go test -race ./internal/webhooks ./tests -run 'Webhook|Enqueuer' -count=1 -timeout 10m` 通过，分别 8.775s、88.392s；日志 `/tmp/webhook-consume-final-race.log`。最后调整的 Session Webhook 失败观察和 10 条在途取消断言另行复跑通过（9.808s），日志 `/tmp/webhook-consume-last-race.log`。
- 最新 `just test` 在重建的专用数据库执行，Webhook 用例通过，但全量未通过：`TestPollCancellationAndConnectionLossAreBounded`、`TestSessionPendingToolRulesMatchAcceptance/child`、`TestTranscriptArchiveRestoreAfterBlobGC` 失败；日志 `/tmp/webhook-consume-just-test-final.log`。首次执行另发现旧 Session Webhook 测试仍检查批次结束后的队列，已修复；调度和 Filestore 的首次失败没有在此次干净库复跑中重现。
- 未修改 HEAD 源码导出到独立目录，用另一专用数据库复核：Session 子线程和 transcript 用例连续三次同样失败；Tunnel 取消用例重复 100 次复现相同的连接关闭错误。日志 `/tmp/webhook-consume-baseline.log`、`/tmp/webhook-consume-baseline-poll.log`。未修改这些模块或跳过门禁。

- 源码生成及 `just lint`、`just dead-code`、`just duplicates`、`just complexity`、`just large-files`、`just hooks-run` 全部通过；日志 `/tmp/webhook-consume-{lint,dead-code,duplicates,complexity,large-files,hooks-run}.log`。未跟踪的两个新文件及最终文档另行显式执行适用 hooks 通过，日志 `/tmp/webhook-consume-new-files-hooks.log`。没有 SKIP 或预算调整；全量 Go 失败单独保留。

- 两个本轮专用测试库已删除，专用测试桶已清理（基线桶未创建）；嵌入式 NATS 和接收器随测试结束停止。用户原有容器、开发数据库和开发桶保持原状。

### Review 与人工顺序

Review 检查了处理位置上限、会话不重叠重启、提前保存 Closed 通知、部分初始化清理、并发 stop、共享 Transport、租户查询和取消后不确认未完成消息。每条消息的 T+15s 从回调开始计时，AckWait 从服务器投递计时；重连可能增加在途消息或重复，统计与确认仍不原子。同步发布、256 MiB / 3000 / 64 KiB 限制、事件目录及生产入口均未改变。

人工顺序：正常投递并验签 → 阻塞 10 条、仅释放一条观察补充 → 慢快端点混合 → 普通及永久失败 → NATS 重连 → 在途停机及恢复。停用当前版本 Worker 后启用新版，继续使用原 Stream/Consumer；本轮无需 migration，不删除 Consumer 或重放消息。

## JetStream 迁移阶段历史记录

以下保留当时的 Fetch/RunOnce 测试与执行事实，消费和连接生命周期已由上面的 Consume 方案替代，旧测试名不再是当前运行入口。

2026-09-29，基于 `76f5d1b`，分支 `codex/webhook-subscriptions`。本轮只迁移投递基础设施，不修改资源业务触发时机、订阅公开 API、前端或 38 项目录（35 项有入口、3 项预留）。代码保留待审核。

## 自动化验收对应关系

| 范围 | 证据 |
| --- | --- |
| 无匹配、禁用、未选中、其他 workspace 不发布；多订阅同一事件 | `TestWebhookNoMatchingSubscriptionDoesNotEnqueue`、`TestWebhookSubscriptionFanoutWithHistoricalJob`、Enqueuer 时间测试 |
| 顺序发布部分成功、期限取消；单条限制、满队列和去重 | `TestEnqueuerPartialFailureAndDeadline`、`TestWebhookQueueLimitsAndPublication` |
| 文件持久化重启、ACK 删除；异常消费耗尽后 TTL 删除 | `TestWebhookQueuePersistenceAndAcknowledgment`、`TestWebhookQueueExhaustionExpiresWithoutReplay` |
| 未确认消息允许重复消费 | `TestWebhookQueueUnacknowledgedRedelivery`；注入 ACK 错误不在原函数重复 HTTP 的单测 |
| 每批 10 条、第 11 条下一批；多实例竞争 | `TestWebhookWorkerConcurrentBatch`、`TestWebhookWorkerMultipleInstances`；阻塞 HTTP 期间数据库连接占用为零 |
| 目标缺失、删除、禁用、跨 workspace | `TestWebhookWorkerTargetIsolation`；消费时读取新 URL/密钥，不重新筛选事件列表由 `TestWebhookWorkerUsesCurrentTarget` 覆盖 |
| 取消、关闭消费及持续响应正文 | `TestWebhookWorkerStopCancelsActiveBatch`、`TestWebhookSubscriptionWorkerStart`、`TestWebhookDeliveryDoesNotWaitForResponseBody` |
| 数据库查询故障不发送；统计失败仍确认成功请求 | `TestWebhookWorkerFailureBoundaries`、真实数据库触发器故障测试 `TestWebhookStatisticsFailureDoesNotRetrySuccessfulHTTP` |
| 重试、永久失败、持续失败禁用、重新启用 | 既有 retry/failure-window 测试迁移至 JetStream；`TestWebhookRetryRealWorkerPolling` 运行真实 Worker 循环，其余 RunOnce 测试不称完整启动验收 |
| 重试保持 ID、payload、事件时间，重新生成签名时间 | `TestWebhookWorkerIndependentFailuresAndRetrySignature`，官方 Go SDK 验签 |
| Session、Vault、Credential、Environment、Memory、Agent、Deployment/Run 入口回归 | 既有资源 Webhook 测试改为真实 JetStream 消息与接收结果断言；保留失败、重复、级联及 OAuth 分类断言，不新增此前待办的全事件回归体系 |
| 旧队列数据收尾 | `TestWebhookQueueRetirementMigration`：只收尾三类活跃 Webhook 任务，保留终态及其他 jobs、原事件和次数；Down 不复活 |

以上为自动化测试，不代表 Console 人工验收、生产集群故障演练或长时间容量/内存压力验收。启动失败清理顺序通过代码 review 和单节点副本配置失败测试核查，未做真实进程逐点故障注入。HTTP ACK 丢失通过测试替身和未确认消息重投分别覆盖，未模拟网络中精确丢弃 ACK 确认包。

## 验证结果

使用独立 PostgreSQL 数据库、每个测试独立的文件存储 JetStream、本地 HTTP 接收器和对象存储测试替身；全仓测试使用单独命名的测试桶。没有运行付费 sandbox，也没有修改开发数据库。

- 定向回归通过：`internal/webhooks`、`internal/config`、`internal/db` 和 `tests`，日志 `/tmp/webhook-targeted-2.log`。最终 Webhook Mapper 与迁移测试通过，日志 `/tmp/webhook-db-final.log`。
- `just test` 已执行但未通过，不能称全部门禁通过。`/tmp/webhook-gate-test.log` 记录三项失败；原 HEAD 导出代码也复现同名失败：`TestUnifySessionResourcesAndFilesMigration`、`TestTranscriptArchivePostgres`、`TestSessionPendingToolRulesMatchAcceptance/child`。前两项对照使用空数据库；首次全仓执行给旧迁移测试使用了已迁移数据库，产生了不同的 fixture 错误，随后已用空库复核，错误与 HEAD 一致。Session 测试在 HEAD 连续三次得到相同 409。
- 基线与空库复核日志：`/tmp/webhook-baseline.log`、`/tmp/webhook-migrations-fresh.log`、`/tmp/webhook-session-baseline.log`。没有修改无关模块或跳过失败项。
- 最终 `go test -race ./internal/webhooks ./internal/config ./internal/db ./tests -run 'Webhook|Enqueuer' -count=1 -timeout 10m` 四包全部通过；集成包 135.614s。包含新增的 HTTP 期间连接占用断言；日志 `/tmp/webhook-final-race.log`。另一次包含已知 Session 失败的 race 命令退出非零，未发现数据竞争；不将该次命令表述为通过。
- 源码生成和 `just lint`、`just dead-code`、`just duplicates`、`just complexity`、`just large-files`、`just hooks-run` 最终全部通过。初次 dead-code 检查发现旧队列遗留的未使用失败原因截断函数，已删除并复跑。日志 `/tmp/webhook-final-*.log`，状态 `/tmp/webhook-final-gates.json`；未跟踪新增文件及最终文档显式执行 hooks 通过，日志 `/tmp/webhook-new-files-hooks.log`。没有通过 SKIP 绕过检查。
- 完成租户隔离、终止分支、失败窗口、持久化残留、连接与 goroutine 生命周期 review。修正了文档中“迁移重置失败窗口”和“每项事件都有独立端到端回归”的歧义。
- 测试接收器与独立嵌入式 NATS 随测试停止；本轮三个 PostgreSQL 测试库及 `oma-webhook-js-20260929` 测试桶已删除。保留用户原有 Docker 服务和开发数据库。Console 人工验收未执行。

## Review 与已知取舍

- 只查询匹配订阅 UUID；消费查询带 workspace、未删除约束。数据库连接不跨 HTTP 请求持有。
- 所有正常终态使用 DoubleAck；连续失败本次禁用也终止当前消息。确认失败不立即重发 HTTP。未知 envelope 结束且不写接收方统计。
- 成功/失败统计保留 enabled 条件，迟到成功不能重新启用订阅。取消或统计失败可能遗漏结果，重复消费可能重复累计；不保留已删除的 jobs fencing 机制。
- 不无限预取或读取响应正文，不新增发布 goroutine；复用既有禁用 reconnect buffer 的共享 NATS 连接。每批 Transport 的空闲连接关闭，Worker 停止等待当前批次后才 Drain NATS。
- 业务提交到发布不原子；满队列、发布失败、TTL 或消费机会耗尽都可能遗漏。最大次数不是精确 HTTP 尝试次数。接收方按事件 ID 去重，并通过资源 API 核实重要状态。
- 64 MiB 为消息逻辑容量，不是进程内存或三副本总磁盘上限。默认消息保留 24h 和默认失败窗口 24h 是不同配置。未增加账本、Outbox、DLQ 或补发服务。

## 升级及人工顺序

1. 停止全部旧版生产者和 Webhook Worker，禁止旧 jobs 和 JetStream 两套实现混跑。
2. 核对 NATS JetStream 可用及各实例配置一致；单节点设置 `nats.webhook_stream.replicas: 1`。默认 3 副本要求集群支持。
3. 运行合并版 migration 70：新增失败窗口字段，并将旧活跃 Webhook jobs 标记 failed，释放锁，不转移到新队列；历史终态、订阅配置及密钥不变。Down 不复活。
4. 启动新版。初始化队列失败则整个启动失败；`worker_enabled=false` 只停消费，生产仍会积压并受容量与 TTL 限制。
5. 创建订阅 → 触发事件并验签 → 普通失败重试 → 永久失败禁用 → 重新启用确认旧消息不恢复 → 停启 Worker 验证积压处理与过期。

回退不会把 JetStream 消息转回 PostgreSQL。变更消费配置采用停机切换，不删除重建 Consumer；提高 MaxDeliver 对异常残留的影响不承诺永久不可恢复。

## 合并迁移与本地协调（同日后续）

按用户确认，未发布的 migration 71 合并进 70，删除 71 文件；旧迁移验证记录保留当时事实。合并后的测试从 69 开始，验证字段新增/撤销与任务收尾，Down 仍不复活旧任务。已执行旧版 70 的库仅补执行收尾 SQL，不删除重建失败窗口、不伪造重新执行记录。

本地 `claude_api` 已有版本 69、70，无 71，字段正确。停止原后端并备份 jobs、webhook_endpoints、goose_db_version 后，在事务中校验版本和字段并补执行收尾 SQL：UPDATE 0。已有 Webhook completed=3、failed=2，其他任务不变；订阅全行摘要和迁移历史摘要前后相同。备份位于本机 `/tmp/oma-webhook-migration-merge-20260929-u2nr0lab/stopped-server-tables.dump`，目录 0700、文件 0600，包含私密配置，不应提交。

合并后重新运行的独立迁移测试通过（`/tmp/webhook-merged-migration-test.log`），源码生成、lint、dead-code、duplicates、complexity、large-files、hooks-run 全部通过（`/tmp/webhook-merge-gates.json`）。全量 `just test` 仍未通过：保留前述两项旧迁移、Session 子线程输入失败；本次还出现调度回收超时与 Filestore 清理任务领取失败。在另一个新空库单独复核后，仅调度测试继续失败，Filestore 未再失败。日志 `/tmp/webhook-merge-test.log`、`/tmp/webhook-merge-isolated-recheck.log`；没有修补无关模块或绕过门禁。

通过 `just restart-server` 重启本地后端，`http://127.0.0.1:38080/healthz` 返回 `{"status":"ok"}`；实际 JetStream 的 Webhook Stream 和 durable Consumer 已建立。开发库最高应用版本为 70、无 71 记录、旧 Webhook 活跃任务为 0。保留本地后端运行供人工验证；本轮三个独立测试数据库和测试桶已清理，开发库和现有容器保留。

## 分支整体 review（2026-09-29 后续）

本轮比较完整工作区与重新 fetch 的 `upstream/main`（`ad8c2df`），包括已提交的订阅/事件接入、未提交 JetStream 实现和新增文件；没有切换 worktree。重点核查是否引入不必要的抽象、资源事务与幂等、租户隔离、队列终态、网络/数据库连接占用、发布与消费的内存边界，以及前端表单和一次性密钥。

确认并修复一项 P2：Vault/Credential、Agent/Deployment 级联和 Session 事件批次逐条入队时，会为每条事件重新获得 5 秒等待时间。队列迟迟不确认会按资源数量累加业务请求耗时。修复使用调用点已有 context，让同组通知共享 5 秒期限；超时后停止后续通知，保持已提交的业务成功，不引入后台 goroutine、补发队列或新的批量事件抽象。定时 occurrence 的关联通知也共享期限，Deployment 租户查询纳入期限。Agent Handler 自身的 archived 通知仍是独立调用。

- `TestWebhookVaultCascadePublicationDeadline` 和 `TestWebhookAgentCascadePublicationDeadline` 在修复前均因缺失整体 deadline 失败；修复后通过。Vault 测试包含发布卡住直到期限到达的场景，确认只尝试首条、停止后续通知且 HTTP 仍成功，父业务 context 不被取消。Session 单测验证已取消的通知批次不再查询数据库。
- 最新定向 race：`internal/webhooks`、`internal/sessions`、`internal/config`、`internal/db`、`tests` 的 `Webhook|Enqueuer` 通过，集成包 132.652s；`internal/deployments` 无匹配单测，其级联回归在 `tests` 中执行。日志 `/tmp/webhook-review-race.log`。此命令未设置迁移数据库变量，迁移用例由后续全仓命令单独启用，不将跳过当成已验证。
- 前端 Webhook 页面 28 项测试通过；全前端格式、命名及构建通过。日志 `/tmp/webhook-review-web-*.log`、`/tmp/webhook-review-web-gates.json`。没有修改前端或扩展为全前端测试通过的结论。
- 使用 `git archive upstream/main` 的独立源码副本和另一个空数据库，对照复现 `TestUnifySessionResourcesAndFilesMigration`、`TestTranscriptArchivePostgres` 和 `TestSessionPendingToolRulesMatchAcceptance/child` 的同一失败；日志 `/tmp/webhook-review-upstream.log`。它们属于此次对比基线问题；没有通过修改无关模块或忽略门禁来隐藏。
- 当前代码全仓 `just test` 已完整执行，失败仅为上面三项 upstream 同现问题；本轮没有再次出现调度回收和 Filestore 清理领取失败。源码生成、lint、dead-code、duplicates、complexity、large-files、hooks-run 均通过；新增文件显式 hooks 通过。日志 `/tmp/webhook-review-test.log`、`/tmp/webhook-review-gates.json`、`/tmp/webhook-review-new-file-hooks.log`。
- 显式设置迁移数据库后，`TestWebhookFailureWindowMigration` 与 `TestWebhookQueueRetirementMigration` 均通过（`/tmp/webhook-review-migrations.log`），确认订阅状态保留、旧任务仅定向收尾及回滚不复活。
- 修复后的第二轮 review 未发现本分支新增的 P0/P1/P2。保留既定取舍：通知尽力发布、统计与确认非原子、可能重复或遗漏、没有顺序保证。没有将这些已确认边界扩大为 Outbox、账本、持续补充并发池或通用事件总线。
- 本轮四个独立 PostgreSQL 测试库和 `oma-webhook-review-20260929` 测试桶已清理；嵌入式 JetStream 与接收器随测试退出。保留原有容器、开发库和开发后端。代码未暂存、提交或推送。

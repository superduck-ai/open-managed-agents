# DSRv1：DSH Session Remote 协议

状态：设计草案（第 7 版），**尚未冻结**，不进入实现。本文定义 `oma-brain-dsh`（下称 DSH）与 OMA 后端之间的执行端协议合同，用于替代当前 Brain 使用的 CCRv2。现行实现见 [沙箱外 Agent 与 CCRv2](../brain-runtime.md)、[Brain Worker 领取](../brain-worker-claim.md) 与 `docs/design/be/ccrv2/`。各接口的请求、响应、状态码与示例见 [DSRv1 接口参考](api-reference.md)。评审依据为 OMA 提交 `64f3332` 与 deepseek-harness 提交 `f3803c9697`（即 [沙箱外 Agent 与 CCRv2](../brain-runtime.md) 记录的 `dsh-v0.2.2-alpha.2` 发布标签所在提交；OMA 通过 npm 固定依赖该发布的包，实现时仍以 lockfile 解析出的版本为准）；修订记录见第 18 节。

冻结条件：第 7（审批）、第 9（输入与 turn）、第 10（公开状态 reducer）三个状态机的不变量经评审确认。第 17 节的待定问题已在第 6 版全部决定，冻结前只需评审这些决定，并确认 15.1 的验收条件。

## 1. 背景与结论

CCRv2 的 Worker 协议来自沙箱内运行的 Claude Code（superduck-code）：`oma-brain-dsh/src/ccrv2/wire.ts` 与 `internal/codesessions/worker_output_schema.go` 直接复刻了 Claude Code 的 `StdoutMessage`、transcript 与 `external_metadata`。DSH 接入后形成"DSH 原生 Session/Event → Claude Code 形状 → CMA 公开事件"的两次翻译，并积累了以下补丁：

| 问题 | 现状 |
| --- | --- |
| 私有日志借道 transcript | `dsh_log` 塞进 transcript 类型，靠拼接 UUID `dsh_log:<cs>:<dsh>:<seq>` 去重；seq 连续性、Header 前缀、冲突检测全部在 DSH 端 |
| 公开输出格式混合 | 同一 `/worker/events` 既有 Claude Code 的 `assistant`/`user`/`result`，又有 CMA 原生事件 |
| 回合与状态隐式 | 公开 running 由 worker 状态上报触发，idle 依赖 `result` 与 `worker_turn_started`，输入拒绝读 `worker_status` 与 `external_metadata` |
| 审批存在竞态补丁 | 发出 `can_use_tool` 后需轮询 `GET /worker` 判断是否进入人工确认；待确认记录存于 `external_metadata`，持久化与公开发布分两步 |
| 工具身份靠命名约定 | 后端从 `mcp__server__tool` 风格名称反解工具类型 |
| Claude Code UI 遗留 | `permission_mode`、`post_turn_summary`、`task_summary` 等字段 |

沙箱内 agent 模式已不再需要，DSH 成为唯一执行端。策略是**保留 CCRv2 的传输内核与执行屏障，替换语义层**。

**收益定位**：边界更清楚、删除兼容代码、把隐式状态变成显式不变量。没有测量依据支持性能收益，本文不以减少往返或降低延迟为目标。

**成本定位**：DSRv1 不只是换词汇，它新增了三个需要单独维护的状态机：trajectory 写入与迁移、审批记录生命周期、公开状态 reducer。第 2 节列出删除与新增的职责对照，作为是否值得实施的判断依据。

## 2. 删除与新增的职责对照

| 删除（或收敛） | 新增（或改写） |
| --- | --- |
| Claude Code 输出 schema 与 mapper 分支（`assistant`/`user`/`result`/`task_*`/`compact_boundary`） | DSR 输出白名单与边界映射（第 8 节） |
| internal subagent transcript 的公开投影 | — |
| `parseClaudeToolIdentity` 与名称反解 | 结构化工具身份（7.1） |
| 审批状态轮询与 `external_metadata` 中的待确认键 | `dsr_permission_requests` 与状态机（第 7 节） |
| `worker_turn_started` 与 worker 状态驱动的公开状态 | `dsr_turns` 与显式 reducer（第 9、10 节） |
| DSH 端 trajectory 前缀对账中的去重与冲突判定 | 服务端条件追加与 `dsr_trajectories` 元数据（第 6 节） |
| `external_metadata` 的自由 merge patch | 有类型的 worker state（4.6） |
| sandbox 模式、`session_ingress/*`、60+10 秒租约参数 | — |
| — | 回填任务与迁移围栏（第 13 节） |

不新建事件表：trajectory 继续存放在 `code_session_internal_events`，增加身份列，从而复用已有的全局 `sequence_num`、对象存储、归档、还原与物理删除机制（6.6）。

## 3. 目标、非目标与兼容边界

目标：

- 私有日志由服务端保证每个 DSH Session 的序号连续、身份一致、内容不可变。
- 审批有持久化记录、确定性身份、终态与接管清理；持久化与公开事件同事务。
- task 输入与 turn 有持久化事实，公开状态由一个显式 reducer 计算。
- 公开输出使用 DSR 事件词汇，后端只做校验、身份派生与 CMA 映射。
- 删除 sandbox 模式与 CCRv2 Worker 路由。

非目标：

- 不改变 JetStream 投递、三段 ACK、Redis ACK 定位与入站消息 30 天逻辑期限。
- 不改变 claim 的领取条件、attempts、耗尽 hold、`FailPermanently` 与长轮询。claim 响应丢失会消耗一次 attempt 并等待领取期限，这是现行已接受的行为，本次不改。
- 不改变沙箱侧（`role=sandbox` JWT）的上游代理、`sign-commit`、MITM 证书接口。
- 不改变 DSH 原生 JSONL 格式、本地主存储与异步复制、恢复语义。
- **不放松执行屏障**（3.1）。
- 不引入 outbox，不改变数据库提交与 NATS 发布之间的现有失败语义（3.2）。

### 3.1 保持不变的执行屏障

1. 工具体执行前，刷新本地日志并等待该工具调用所在前缀的 trajectory 远程确认；任一失败时不执行工具体。
2. 公开消息、工具结果与回合终态发布前，等待其来源事件的 trajectory 远程确认。
3. 持久公开输出在发送前，等待捕获时已有的流式预览确认。
4. task 输入按"trajectory 终态确认 → 公开输出确认 → `complete` 确认 → `ack processed`"的顺序结束。
5. 结果已到达但复制未确认时，不得 release 领取。
6. 模型请求前不无条件等待复制。

"远程确认"指追加接口返回的 `next_seq` 已覆盖目标事件。

### 3.2 提交、发布与接收

服务端 `2xx` 只表示 **PostgreSQL 事务已提交**，包括同事务写入的公开事件行。之后经 NATS 扇出到 SSE；发布失败沿用现有语义（不重试发布，客户端经历史与游标重放获得）。DSH 不得把 `2xx` 理解为公开客户端已收到。连接在收到响应前中断时，DSH 按幂等键重试，服务端返回首次结果。

### 3.3 公开 API 的兼容边界

公开事件类型、字段和 SSE 合同不变。但状态来源改变后，以下行为的**时序与归属**会变化，属于需要单独验收的兼容边界：

- `session.status_running` 由服务端在输入接纳、审批解决与接管时发布，不再由 worker 上报触发；公开 running 可能早于 worker 实际开始执行。
- `session.status_idle(requires_action)` 的发布时刻改为审批记录提交时，恢复路径由审批状态机决定。
- 状态事件 ID 改为确定性派生（10.4），重试不再追加内容相同而 ID 不同的状态事件。
- webhook 的 idle 映射与 `stop_reason.event_ids` 的取值来源改为审批表与 turn 表。
- **等待审批时执行端崩溃**：现行会话停在 `requires_action`，直到用户响应才触发重新领取，而恢复后并不会执行该工具。DSRv1 在租约过期后主动领取（4.3），新 owner 将待确认审批标为 `superseded`，并以 `recovered` 结束该输入。用户看到的审批请求会被撤回，取而代之的是恢复提示与 idle。

## 4. 通用约定与生命周期

### 4.1 路径与版本

- 执行端接口挂载在 `/v1/dsr/` 下，按 chi 子路由注册，不走 `/v1/*` workspace API key 鉴权回退。
- 每个请求携带 `DSR-Protocol: 1`。缺失或不支持返回 `400`，`DSR-Error: unsupported_protocol`。
- **词汇由协议版本固定，不在运行时协商。** v1 的输出类型（8.2）与输入 kind（9.1）以本文白名单为准。任何增删或字段语义变化都必须发布新的协议版本（例如 `DSR-Protocol: 2`），不在 v1 内追加。
- **版本绑定到会话**：register 把请求头中的版本记录到 Code Session（`metadata.config.dsr_protocol_version`）。此后该 epoch 的所有请求必须携带同一版本，否则返回 `400 unsupported_protocol`；后端只向该会话投递该版本定义过的输入 kind。新 epoch 注册时可以换成另一个受支持的版本。
- **部署顺序**：新版本先在全部后端实例上线（后端可同时支持多个版本，按请求头分派），确认没有旧实例后再升级 DSH 使用新版本；下线旧版本前，确认没有会话仍记录旧版本。这与 13.3 的"后端先全量升级"是同一条纪律，因为版本支持是全后端统一的规则，不依赖单个实例在某次请求中的回答。
- 因此 `/output` 对未知类型严格返回 400 不会在升级中误伤：DSH 只发送它注册时所用版本定义的类型，而所有后端实例都已支持该版本。
- DSR 自有的 ID、epoch 与游标（`next_seq`、`from_seq`、`next_from_seq`）在 JSON 中为字符串。内嵌的原生 Header/Event 保持原样，`SessionEvent.seq` 仍是 number，服务端校验它是非负安全整数（≤ 2^53 − 1）。

### 4.2 鉴权

| 凭据 | 用途 | 说明 |
| --- | --- | --- |
| fleet token | `claims`、`release` | 沿用 `code_session.brain_claim_token_file` |
| session token | `/v1/dsr/sessions/{code_session_id}/*` | claim 签发的 JWT，claims 含 `code_session_id`、`epoch`、`protocol: "dsr1"` 与租户身份 |
| model token | `/v1/messages` | 沿用现行 |

epoch 只来自 session token，请求体不携带 `worker_epoch`；服务端在执行授权阶段与写事务内做 fence。CCRv2 路由拒绝 `protocol: "dsr1"` 的 token，DSR 路由拒绝不带该 claim 的 token。

### 4.3 Claim 与 Release

- `POST /v1/dsr/claims?block_ms=25000`：请求与响应字段同现行 claim（`session_id`、`worker_epoch`、`ingress_token`、`model_token`、`claim_expires_at`、`trace_parent`），token 增加 `protocol` claim。**不合并注册，不内嵌 context。**
- 领取 SQL 增加 `metadata.config.worker_protocol = 'dsr1'`；`migrating` 状态的会话不被任何一侧领取（13.2）。
- **可领取条件增加未结算 turn**：除现行的"存在领取请求或公开会话 running/rescheduling"外，若租约加宽限已过，且存在以下任一 turn，也可领取：
  - `processing` 且非 terminal（等待审批时崩溃：公开会话为 `idle(requires_action)`，现行条件不会命中，注册时的接管清理（4.4）也就无法触发）；
  - terminal 但 `acked_at` 为空（`complete` 已提交、`ack processed` 前崩溃：公开会话可能已经 idle，未 ACK 的输入仍需新 owner 补 ACK）。
- 这一扩展仍排除：`budget_reached` 暂停中的会话、`worker_claim_expires_at` 未到期的耗尽 hold 与退避行、非 active 或已终止的 Code Session。
- 旧 claim 接口**必须**同时增加 `worker_protocol IS NULL OR worker_protocol = 'ccrv2'`，否则新 DSH 以 `idle` 释放后，旧 DSH 可以重新领取并写入旧路径，造成 split-brain。这两个过滤属于 P1 的正确性要求。
- `worker_claim_expires_at` 保持现有职责：领取到注册的窗口，以及 `HoldExhausted`、`FailPermanently`、`admission_failed` 退避的停车列，`ResetExhausted` 与指标照常读取。
- `POST /v1/dsr/claims/{code_session_id}/release`：同现行，字段名不变。

### 4.4 Register

`POST /v1/dsr/sessions/{id}/register`，请求体 `{}`。

- 语义同现行 `worker/register`：同一 epoch 允许重复注册，在同一事务内清空领取请求与期限，写入租约与连接状态。
- **410 恢复路径**：租约过期但 epoch 未被推进时，DSH 以同一 epoch 重新 register 恢复租约，不需要重新 claim 和全量恢复；只有 `409 epoch_superseded` 才需要退役。
- **接管清理**：当本次注册的 epoch 高于审批记录或 turn 记录上的 epoch 时，同一事务内按 7.6 关闭旧 epoch 的 pending 审批，并按 10.3 重新计算公开状态。open turn 不在注册时结束：它仍是 `processing`，未 ACK 的输入会重投给新 owner，由新 owner 按原生日志恢复后调用 `complete`（通常是 `recovered`）。
- 两种协议的 register 都拒绝 `worker_protocol = 'migrating'` 的会话（`409 trajectory_migrating`）。
- 响应：

```json
{
  "epoch": "7",
  "protocol": "1",
  "lease": { "ttl_ms": 20000, "grace_ms": 10000, "heartbeat_interval_ms": 6000 }
}
```

### 4.5 Heartbeat 与 Context

- `POST /v1/dsr/sessions/{id}/heartbeat`，请求体 `{}`，响应 `{"lease_expires_at": "..."}`；`409 epoch_superseded`、`410 lease_expired`。
- `GET /v1/dsr/sessions/{id}/context`，可选 `DSR-Thread-ID`；内容同现行 `session_context`，`Cache-Control: no-store`。

### 4.6 Worker state

`external_metadata` 替换为有类型的 worker state。先列依赖清单再删除：

| 现用途 | 读写方 | DSRv1 去向 |
| --- | --- | --- |
| `model`：恢复用户最后选择的模型 | DSH 写、DSH 恢复时读 | worker state `model` |
| `oma_trace_turns`：恢复 trace 连续性 | DSH 写与读 | worker state `trace_turns` |
| `managed_agent_tool_permission_request:*`、`pending_action(s)` | 后端写，输入拒绝、`stop_reason.event_ids`、DSH 轮询读 | `dsr_permission_requests`（第 7 节） |
| `task_summary`、`post_turn_summary`、`permission_mode` | 无实际读者 | 删除 |
| `worker_status` | 公开状态同步、输入拒绝、sandbox lifecycle SQL、telemetry | 保留列，值域收敛为 `idle`/`running`，**不产生公开副作用**；lifecycle SQL 改为 `IN ('idle','running')` |

实现前用代码搜索确认 `external_metadata`、`worker_status`、`worker_turn_started` 的全部读点已列入本表。

接口：

- `GET /v1/dsr/sessions/{id}/state` → `{"status": "idle"|"running", "model": "...", "trace_turns": {...}}`。
- `PUT /v1/dsr/sessions/{id}/state`：字段级 patch，只接受上述字段，未知字段 400。

### 4.7 错误

错误体保持 `internal/httpapi.WriteError` 的 Anthropic 兼容形状。机器可读的分支由响应头承载：`DSR-Error` 给出 code，`DSR-Next-Seq` 在 trajectory 错误时给出服务端水位。

| HTTP | `DSR-Error` | DSH 处理 |
| --- | --- | --- |
| 400 | `invalid_request` | 视为缺陷，不重试 |
| 400 | `unsupported_protocol` | 停止进程 |
| 400 | `unsupported_trajectory_version` | 以 `permanent` 释放会话（6.9） |
| 401/403 | `unauthorized` | 停止该会话 |
| 404 | `session_not_found` | 退役会话 |
| 409 | `epoch_superseded` | fence 主线程与全部子线程，停止写入 |
| 409 | `trajectory_header_required` | 先写 Header 再追加 |
| 409 | `trajectory_gap` | 从 `DSR-Next-Seq` 补发 |
| 409 | `trajectory_conflict` | 停止恢复，保留本地文件 |
| 409 | `trajectory_migrating` | 释放为 `shutdown`，稍后重试 |
| 409 | `permission_idempotency_mismatch` | 视为缺陷，停止该会话 |
| 409 | `output_idempotency_mismatch` | 视为缺陷，停止该会话 |
| 409 | `turn_idempotency_mismatch` | 视为缺陷，停止该会话 |
| 409 | `input_not_eligible` | 视为缺陷，停止该会话 |
| 409 | `input_not_completed` | 不 ACK，先完成 `complete` |
| 410 | `lease_expired` | 同 epoch 重新 register（4.4） |
| 410 | `trajectory_purged` | 退役会话，不再恢复（6.8） |
| 413 | `payload_too_large` | 本地日志尚未写入时：当前输入以 `failed` 结束；已写入不可复制的事件时：以 `permanent` 释放会话（6.5） |
| 429/5xx | — | 有界退避重试 |

- 缺少 `DSR-Error` 的 4xx（例如中间代理生成的错误）按 `epoch_superseded` 的保守路径处理：fence、停止写入、不 release，等待租约过期后重新领取。
- 429/5xx 重试耗尽的行为沿用现行 CCRv2 客户端对同一路径的处理：屏障路径耗尽时不执行后续工具，当前输入以 `failed` 结束；`complete` 或 heartbeat 耗尽时停止会话。
- 错误合同集中在承载 DSR 的资源包根目录 `errors.go`。

## 5. 身份词汇

| 名称 | 层 | 含义 | 是否公开 |
| --- | --- | --- | --- |
| `session_id`（`sesn_*`） | 公开 API | Managed Agents Session | 是 |
| `session_thread_id`（`sthr_*`） | 公开 API | 公开线程；主线程与子 Agent 线程 | 是 |
| `code_session_id`（`cse_*`） | 执行端 | 一个公开 Session 的执行容器，epoch、租约与 trajectory 的作用域 | 否 |
| `dsh_session_id` | 执行端 | DSH 原生 Session ID；主线程等于 `code_session_id`，子线程为原生 subagent Session ID（即现行传输层 `agent_id`） | 否，不是新的公开身份 |
| trajectory | 执行端 | 一个 `dsh_session_id` 的 Header 与 Event 序列 | 否 |
| `input_id` | 执行端 | task/control 输入的传输稳定 ID（5.1） | 否 |
| `request_id` | 执行端 | 审批记录 ID，确定性派生（7.1） | 否 |

映射关系：子线程的公开 `session_thread_id` **等于**其 `dsh_session_id`（现行 `ccrv2/subagents.ts` 直接以 DSH 生成的 UUID 作为线程 ID），主线程的 `session_thread_id` 是公开 Session 的主线程，`dsh_session_id` 等于 `code_session_id`。该对应关系在子线程的 `session.thread_created` 被接受时固定，之后不可变（8.2）；`parent_dsh_session_id` 必须与原生 `header.parentSession` 一致，服务端校验两者相等。

## 6. Trajectory（私有日志）

### 6.1 资源模型

- 每个 `dsh_session_id` 一条 trajectory，由一个 Header 与**从 0 开始**连续递增的 Event 组成，序号即原生 `SessionEvent.seq`。
- 服务端维护 `next_seq`（已确认条数，空日志为 `"0"`；确认 seq 0、1 后为 `"2"`）与 `header_present`，两者独立表达。

### 6.2 服务端校验的原生身份字段

| 对象 | 字段 | 规则 |
| --- | --- | --- |
| Header | `id` | 等于路径 `dsh_session_id` |
| Header | `parentSession` | 主线程缺省；子线程等于 `parent_dsh_session_id` |
| Header | `version` | 正整数；是否受支持由 DSH `assertVersion` 判定 |
| Event | `seq` | 非负整数，唯一序号来源，请求中不另设外层 seq |
| Event | `type` | 非空字符串 |

### 6.3 比较语义

- 比较定义在 **wire 值的语义**上，与 DSH 现行 `json-equality.ts` 一致：对象键无序，数字按 IEEE 754 双精度数值相等（`1`、`1.0`、`1e2`/`100` 分别相等），字符串按解码后的码点比较（`\uXXXX` 与字面字符相等）。
- 摘要使用 SHA-256 over [RFC 8785 JCS](https://www.rfc-editor.org/rfc/rfc8785)。JCS 恰好实现上述语义，并对重复键报错（返回 `400 invalid_request`）。
- 摘要只用于快速判定；**摘要不同时，服务端回读原行做语义比较再决定是否冲突**，避免历史数据的表示差异造成误判。
- 超过 2^53 的整数在两端都会丢失精度；原生日志不应包含此类数值，样本测试覆盖该边界。
- TS 与 Go 共用样本文件（键序、`1`/`1.0`、指数、转义、Unicode、嵌套、空对象/数组、大整数），两端测试必须得到相同摘要。

### 6.4 写 Header

`PUT /v1/dsr/sessions/{id}/trajectories/{dsh_session_id}/header`：

```json
{ "parent_dsh_session_id": "cse_...", "header": { "...": "原生 SessionHeader" } }
```

- 幂等比较覆盖 `(parent_dsh_session_id, header)`：全部相同返回 `200 {"dsh_session_id", "next_seq"}`，任一不同返回 `409 trajectory_conflict`。
- 子线程的 parent 必须是同一 Code Session 下已存在的 trajectory。
- **Header 不要求公开线程已存在。** 现行创建路径先刷新含子线程 Header 的日志并等待远程确认（屏障 2），再发布 `session.thread_created`；若 Header 依赖公开线程，就会形成循环依赖。依赖方向因此固定为：Header 先写入，`session.thread_created` 被接受时校验对应 trajectory 已存在并绑定线程（8.2）。

### 6.5 追加

`POST /v1/dsr/sessions/{id}/trajectories/{dsh_session_id}/events`，请求体 `{"events": [<原生 SessionEvent>, ...]}`。

**一个请求只属于一条 trajectory**。DSH 现行上传队列按 root code session 组织，一个批次可能混合主线程、子线程与子线程 Header；DSR 客户端必须按 `dsh_session_id` 拆分队列，每条 trajectory 内保持顺序，Header 先于 Event。不同 trajectory 之间没有顺序要求。

事务（all-or-nothing）：

1. 锁定 Code Session 并校验 token epoch；不匹配返回 `409 epoch_superseded`。
2. 锁定 `dsr_trajectories` 行；Header 不存在返回 `409 trajectory_header_required`。
3. 请求内 seq 必须连续递增。`seq < next_seq` 的条目按 6.3 与已存条目比较：相同则跳过，不同返回 `409 trajectory_conflict`。已存条目可能已归档并从 PostgreSQL 软删除，比较使用合并读回路径（6.7），不能因为在 PostgreSQL 中找不到就当作新条目写入。`next_seq` 只增不减，不随归档、软删除或还原改变。
4. 第一条未确认条目必须等于 `next_seq`，否则返回 `409 trajectory_gap`，并在 `DSR-Next-Seq` 中给出当前值。
5. 超过 32 KiB 的原始 JSON 走 `eventpayload` 外置；插入 internal event 行，分配全局 `sequence_num`，更新 `next_seq`。

响应 `200 {"next_seq": "42"}`。

大小限制：

- 单条原生事件的原始 JSON 上限 **15 MiB**，请求体上限沿用 ingress 的 16 MiB，留出 1 MiB 给请求封装；单条超限返回 `413`，不靠拆批解决。
- **超限必须在写入本地日志之前拦截。** DSH 在追加原生事件前检查大小；工具结果等大内容沿用现有的外置与截断（例如外部 MCP 大结果落盘），使事件不超过上限。
- 若超限事件仍进入了本地不可变日志，它和其后的所有事件都无法复制，屏障 1、2、4 再也无法满足。此时不能只把当前输入标为 `failed`：DSH 以 `permanent` 释放会话，后端按现有永久失败路径发布 `session.error` 与 `idle(retries_exhausted)`，该会话不再被领取。

### 6.6 存储

trajectory 存放在现有 `code_session_internal_events`，新增 goose migration：

- `code_session_internal_events` 增加 `dsh_session_id text null`、`dsh_seq bigint null`，以及唯一索引 `(code_session_uuid, dsh_session_id, dsh_seq) WHERE dsh_session_id IS NOT NULL`。索引**不以 `deleted_at` 为条件**，否则归档软删后同序号可以再次插入。Header 行 `dsh_seq` 为空，由元数据表引用。
- 行身份沿用现行规则：`event_type = 'dsh_log'`，幂等键与 payload UUID 为 `dsh_log:<code_session_id>:<dsh_session_id>:<seq>`（Header 为 `:header`，各段 `encodeURIComponent`），子线程的 `agent_id` 等于 `dsh_session_id`。`payload` 直接是原生对象，不再包装 `{type, uuid, data}`。
- **归档保真**：archive codec 升级为 v2，`DecodedEvent` 增加 `dsh_session_id` 与 `dsh_seq`，还原时原样写回。v1 段（含回填前的历史段）按幂等键无损重建两列，并校验重建出的 seq 等于 payload 中的 `seq`；不一致视为损坏，还原失败。v1 段保持可读。
- 全局 `sequence_num`、`payload_blob_uuid`、`payload_hash`、`deleted_at`、`uuid` 与租户列保持现有含义；归档分段、物理删除与 blob GC 继续按全局 `sequence_num` 工作，`transcript_archives` 的唯一键不变，codec 只做上述字段扩展。
- 新增元数据表，不创建外键：

```text
dsr_trajectories
  id bigint generated always as identity
  uuid uuid default gen_random_uuid()
  organization_uuid, workspace_uuid, code_session_uuid uuid not null
  dsh_session_id text not null
  parent_dsh_session_id text null
  session_thread_id text null          -- thread_created 被接受时写入，之后不可变
  header_event_uuid uuid null          -- 指向 code_session_internal_events.uuid
  next_seq bigint not null default 0
  migration_state text not null        -- legacy | migrating | ready
  migration_checkpoint jsonb null
  created_at, updated_at, deleted_at
  unique (code_session_uuid, dsh_session_id)
```

- 该表不对外暴露，不设 `external_id`；`id` 只承担 identity。若评审认为它属于核心业务表，再补 `external_id`。
- Mapper 按 [Yourbatis 指南](../yourbatis-guidelines.md) 拆分文件；bytea、jsonb、nullable 与部分唯一索引需要真实 PostgreSQL 测试。

### 6.7 读取

- `GET /v1/dsr/sessions/{id}/trajectories` → `{"data": [{"dsh_session_id","parent_dsh_session_id","session_thread_id","header_present","next_seq"}]}`。
- `GET /v1/dsr/sessions/{id}/trajectories/{dsh_session_id}?from_seq=0&limit_bytes=4194304` → `{"header": {...}|null, "events": [...], "next_from_seq": "37"|null}`。
  - `from_seq` 包含该序号，`header` 只在 `from_seq = 0` 时返回。
  - `limit_bytes` 是软预算（上限 8 MiB），按原始字节累计，Header 计入预算。每页至少返回一条；单条超过预算时该页只含这一条，响应仍可达 15 MiB 加封装。
  - 使用现有"归档 + PostgreSQL"合并读回与外置 payload 还原路径；已归档行不会被判为缺口。
- 定位：`dsr_trajectories` 只记录水位，不记录每条事件的位置。PostgreSQL 行已物理删除时，读取按 Code Session 扫描其归档段，再按 v2 字段或幂等键（6.6）过滤出目标 trajectory。扫描成本与会话总日志量成正比，可以接受；若测量证明不可接受，再为归档段增加 `dsh_session_id` 索引。
- GET 只做鉴权与归属校验，不要求当前 epoch。旧 epoch 的 token 可以读，不能写。
- **接管时的前缀核对保留不变**：服务端 `next_seq` 只能证明远端长度，不能证明内容一致。DSH 现行流程（读取并校验远程历史，判断本地与远端是否互为前缀，再水合或追加）照常执行；本地更长且已确认部分内容不同的情况，仍按现行规则报错并保留本地文件。

### 6.8 保留与压缩

- trajectory 是长期日志，沿用 internal events 的保留、归档与物理删除规则，不适用入站消息的 30 天期限。
- 需要区分两种删除：**归档后删除 PostgreSQL 行**（归档段仍是完整副本，读取、幂等比较与还原照常工作）与**删除最后一份副本**（归档段或对象也被删除）。后者发生后，`dsr_trajectories` 标记为 `purged`：读取返回 `410`，`DSR-Error: trajectory_purged`；追加返回同一错误；领取 SQL 排除该会话。不能返回看似完整、实际有缺口的日志。
- 不再使用 `is_compaction` 过滤；原生压缩事件是普通 Event。

### 6.9 DSH 原生格式升级

trajectory 一经确认不可改写，而 DSH 升级可能改变 Header 结构、版本号或 seq 的生成方式（例如上游 Session 格式迁移在读取时重写记录）。约束如下：

- 区分两种迁移：OMA 传输层的回填（第 13 节，不改写 payload）与 DSH 原生格式迁移（例如上游 v3 到 v4 的迁移会修改 Header、插入事件并重新映射 seq）。后者产生的是"同一 Session 的另一份日志"，不能接续到已确认的 trajectory 上。
- **DSRv1 只支持固定的原生格式版本 4**，与当前 `0.2.2-alpha.2` 一致。Header PUT 校验 `version` 在服务端允许列表 `[4]` 内，否则返回 `400 unsupported_trajectory_version`；不接受迁移后的接续写入。
- 一条 trajectory 的 Header `version` 在创建时固定。DSH 升级后，已有 trajectory 继续按原版本追加。
- 支持新的原生版本需要引入按 generation 划分的 trajectory（同一 `dsh_session_id` 下多代日志，旧代只读），属于 DSRv2 的范围。
- DSH 可以在内存中把旧版本记录升级后使用，但**不得把改写后的 Header 或 Event 重新上传**。服务端对已确认序号的任何差异都按 6.5 判为 `trajectory_conflict`。
- 需要改写已确认记录或改变 seq 语义的格式变更，与 DSRv1 不兼容。这类变更必须先给出显式迁移方案（新 trajectory 版本、离线转换工具与回滚），评审通过后才能升级 DSH 依赖。
- DSH 依赖升级的检查清单增加一项：对照上游 [Session 格式与发布状态](https://github.com/deepseek-ai/deepseek-harness/blob/f3803c96974ad4a94195fb97e5c637e9c2a3afe3/docs/session-format-status.zh.md)，确认新版本能以原版本继续追加已有日志。

## 7. 工具审批

### 7.1 请求

`POST /v1/dsr/sessions/{id}/permissions`：

```json
{
  "input_id": "<input_id>",
  "turn": "3",
  "dsh_session_id": "cse_...",
  "tool_use_id": "toolu_...",
  "tool": { "kind": "builtin" | "mcp" | "custom" | "delegation", "name": "bash", "server": "github" },
  "input": { "command": "npm test" },
  "available": true
}
```

- **身份**：`request_id = "dsrp_" + base32(sha256(code_session_id, dsh_session_id, tool_use_id))`，由服务端派生并在响应中返回；DSH 用同样规则在本地计算。审批按 `(dsh_session_id, tool_use_id)` 去重，恢复者无需知道旧进程的随机 ID 就能取消或查询。
- **归属**：`input_id` 必须是该 Code Session 下处于 `processing` 的 task turn；`dsh_session_id` 对应的线程必须是该输入的目标线程，或同一 Code Session 内的子线程。不满足返回 `409 input_not_eligible`。
- **子线程执行与输入的关联**：同一子线程可以被多次委派，静态的 parent 关系只能证明它属于该会话，不能证明某次调用属于当前输入。DSRv1 以 DSH 声明的 `input_id` 为准；服务端能校验的只有"该输入正在 processing"和"线程在同一会话树内"。由于 DSH 串行处理 task 输入，同一时刻至多有一个 processing turn，这一约束足以排除跨输入串扰。
- **接管不继承审批**：新 owner 不恢复旧 owner 的等待项（4.4 把它们标为 `superseded`），恢复后的新调用使用新的 `tool_use_id`，从而得到新的 `request_id`。
- **工具身份**：
  - `builtin`：DSH 内置工具，按 Agent 快照的内置工具集策略判定。
  - `mcp`：外部 MCP 工具，`server` 必填。
  - `custom`：用户定义工具，结果来自公开 `user.custom_tool_result`。
  - `delegation`：`list_agents`、`subagent`、`send_message`、`interrupt_agent`，权限按现行 `delegationToolPermission` 由 multiagent 配置推导。
  - 自托管环境的外部执行（现行 `external && agent_toolset`）由服务端根据环境判定，不由 DSH 声明；判定后与 custom 走同一等待结果路径。
- DSH **在发送请求前**于本地登记等待项（现行已如此），提前到达的 `permission.resolved` 也能完成等待。

### 7.2 记录状态机

```mermaid
stateDiagram-v2
    [*] --> resolved: 创建时判定 allow / deny
    [*] --> pending: 需人工确认或等待外部结果
    [*] --> cancelled: 取消先于创建（墓碑）
    pending --> resolved: 用户确认 / 外部结果
    pending --> cancelled: DSH 取消
    pending --> closed: 所属 turn 结束
    pending --> superseded: 更高 epoch 注册
```

- 每个 `request_id` 一行；所有转换都是对同一行的条件更新（`WHERE state = 'pending'`），竞争时只有一个成功，失败方读取并返回当前终态。
- 终态（resolved、cancelled、closed、superseded）不可再转换。

```text
dsr_permission_requests
  id bigint generated always as identity
  uuid uuid default gen_random_uuid()
  organization_uuid, workspace_uuid, code_session_uuid uuid not null
  request_id text not null
  input_id text null                   -- 墓碑可为空
  turn bigint null
  dsh_session_id text not null         -- 墓碑也必填，由取消请求提供
  session_thread_id text null
  tool_use_id text not null            -- 墓碑也必填，由取消请求提供
  request_sha256 bytea null
  state text not null
  decision jsonb null
  public_event_id text null
  worker_epoch bigint not null
  created_at, updated_at
  unique (code_session_uuid, request_id)
```

### 7.3 创建与响应

一个事务完成：锁定公开 Session 与 Code Session（顺序同输入接纳）→ 校验 epoch 与归属 → 计算权限 → 写记录 → **在同一事务内写入公开事件行**（`agent.tool_use`、`session.status_idle(requires_action)` 等）→ 按 10.3 计算状态。事务失败返回 5xx，DSH 幂等重试；不存在"已返回 202 但公开事件缺失"或"公开 requires_action 但没有记录"的中间态。这要求把现行 `persistToolPermissionRequest` 与 `publishWorkerPublicPayloads` 合并为一个 Yourbatis 事务。

| 情形 | 响应 |
| --- | --- |
| 新请求，策略 allow | `200 {"request_id", "state": "resolved", "decision": {"behavior": "allow"}}` |
| 新请求，策略 deny | `200 {"request_id", "state": "resolved", "decision": {"behavior": "deny", "message": "..."}}` |
| 新请求，需人工确认 | `202 {"request_id", "state": "pending"}` |
| 新请求，custom 或外部执行 | 先按 deny/ask 策略判定，顺序与现行 `handleToolPermissionRequest` 一致；未被拒绝时 `202 pending` |
| 子线程已终止 | `200 resolved/deny`，沿用现行 terminated-thread 拒绝 |
| 重试，摘要相同 | 返回当前状态；`resolved` 附 `decision`（含 `external_result`），终态附 `state` |
| 重试，摘要不同 | `409 permission_idempotency_mismatch` |
| 已有墓碑 | `200 {"state": "cancelled"}`，不写公开事件 |

校验顺序：鉴权与 epoch → **按 `request_id` 查找已有记录**（墓碑或任何终态、pending，命中则按上表返回，不再做后续校验）→ 新建前的归属校验（`input_id` 处于 processing、线程归属）→ 权限计算。终态重试和墓碑因此不会因为输入已不再 processing 而被拒绝。

- 重试返回首次判定，不随后续策略变化改变。执行时的重验由 `/sandbox/mcp` 网关负责（每次 `tools/call` 重算，现行行为）；不经过网关的工具以本接口判定为准，与现行一致。
- 云沙箱内策略为 `allow` 的内置工具继续由 DSH 本地放行，不调用本接口。

### 7.4 解决

用户确认或外部结果到达时，公开 API 在事务内把记录从 `pending` 改为 `resolved`，**并在同一事务内按 10.3 重新计算状态**；全部待确认解决后由该事务发布 `session.status_running`。更新失败（记录已是其他终态）时按现行"工具确认已过期"处理公开请求，不投递。成功后经 control lane 投递：

```json
{
  "kind": "permission.resolved",
  "request_id": "dsrp_...",
  "resolution": { "behavior": "allow" | "deny" | "external_result", "updated_input": {}, "message": "...", "external_result": {} }
}
```

DSH 收到时若本地等待项不存在（已取消、turn 已结束或属于旧 epoch），只 ACK，不执行工具。投递丢失时，DSH 可用 7.3 的重试取得终态。

### 7.5 取消

`POST /v1/dsr/sessions/{id}/permissions/{request_id}/cancel`：

```json
{ "dsh_session_id": "cse_...", "tool_use_id": "toolu_...", "input_id": "<input_id>" }
```

- `request_id` 不可逆，服务端无法从它还原身份，所以请求体必须提供 `dsh_session_id` 与 `tool_use_id`。服务端按 7.1 重新派生，结果与路径不一致返回 `400 invalid_request`。`input_id` 可选，提供时按 7.1 校验归属。
- `pending` 改为 `cancelled` 并重新计算状态；记录不存在时用这些字段插入墓碑；已是终态时返回当前终态。
- DSH 在中断与停机放弃等待时调用。

### 7.6 自动关闭

以下事务内，服务端关闭 pending 记录并按 10.3 重新计算状态：

| 触发 | 关闭范围 | 终态 |
| --- | --- | --- |
| `complete`（9.3） | 该 `input_id` 下的 pending | `closed` |
| 公开 `user.interrupt` 被接纳 | 目标线程（缺省为全部线程）的 pending | `closed` |
| 更高 epoch 注册（4.4） | `worker_epoch` 低于新 epoch 的 pending | `superseded` |
| 子线程终止 | 该线程的 pending | `closed` |

接管清理保证：DSH 崩溃后新进程即使不知道旧审批，公开状态也不会停在 `requires_action`，新 `user.message` 不会因残留记录被持续拒绝。

## 8. 公开输出

### 8.1 接口

`POST /v1/dsr/sessions/{id}/output`：

```json
{ "events": [ { "id": "<client idempotency key>", "type": "agent.message", "session_thread_id": "sthr_...", "...": "..." } ] }
```

- `id` 是**客户端幂等键**，必填；公开事件 ID 由服务端从它稳定派生（沿用现有派生规则），两者不同。新端点有独立的请求 schema，不复用要求 `uuid` 的 CCRv2 校验。
- 每批 1–200 条，all-or-nothing：任一条校验失败则整批 400，不部分写入。同 `id` 不同内容返回 `409 output_idempotency_mismatch`。
- 响应 `200 {"accepted": <新写入条数>, "duplicates": <因 id 已存在且内容相同而跳过的条数>}`。
- 类型白名单由会话注册时的协议版本决定（4.1），未知类型返回 400。

### 8.2 词汇与边界映射

| DSR `type` | 字段 | 服务端映射 |
| --- | --- | --- |
| `preview.start` / `preview.delta` | `event: {type, id}` / `event_id`, `delta` | 映射为现行 `event_start` / `event_delta`，进入 `CategoryFor`、`IsStreamDelta` 与 `messageStreamPreview` 的既有识别路径；映射在边界完成，内部类型不新增 |
| `agent.message` / `agent.thinking` | `content?` | 原样 |
| `tool.result` | `tool_use_id`、`tool`、`is_error`、`content` | `agent.tool_result` / `agent.mcp_tool_result` |
| `system.message` | `content` | 原样 |
| `session.thread_created`、`session.thread_status_running`/`_idle`/`_terminated` | CMA 字段 | 原样 |
| `agent.thread_message_sent`/`_received`、`agent.thread_context_compacted` | CMA 字段 | 原样 |
| `session.error` | `error`（仅 MCP 连接/鉴权失败类） | 原样 |

- `session.thread_created` 被接受的前提：对应 `dsh_session_id` 的 trajectory 已存在，且其 parent 与事件中的 `parent_thread_id` 一致；接受时把 `session_thread_id` 写入 `dsr_trajectories`。重复的同一事件幂等，同一 trajectory 绑定不同线程返回 `409 output_idempotency_mismatch`。
- 线程状态与线程消息事件的 `id` 必须由原生身份确定性派生（例如 `<thread>_<kind>_<turn>_<seq>`），替代现行 `crypto.randomUUID()`，以便重启后补发时去重。
- `system.message` 也是客户端可提交的输入类型（用于拒答问题的结算）。两者以写入路径区分：本端点的写入不进入输入接纳与 Worker 投递，这与现行 `/worker/events` 一致，DSRv1 不引入新的冲突。
- 删除 Claude Code 的 `assistant`、`user`、`result`、`task_started`、`task_notification`、`compact_boundary`，以及从 internal subagent transcript 投影公开事件的路径。

### 8.3 双写者合同

模型代理与 DSH 都会写入助手消息相关事件，DSRv1 把现有的隐式规则写成显式合同：

| 事件 | 写者 | 规则 |
| --- | --- | --- |
| 模型流产生的 `agent.message`、`agent.tool_use`、`span.model_request_*` | 模型代理 | 主写者，按 `StableAssistantEventID` 派生 ID |
| 流式预览、完成块的 `agent.message`/`agent.thinking` | DSH `/output` | 使用与模型代理相同的稳定 ID；先到者写入，后到者按 echo hash 比较，相同则视为重复，不同则记录告警并保留先到者 |
| 工具结果、线程事件、`system.message` | DSH `/output` | 唯一写者 |
| 状态事件 | 服务端 reducer（第 10 节） | 唯一写者 |

两侧之间没有顺序保证；SSE 消费方按现行规则去重。

## 9. 输入与 turn

### 9.1 输入身份与种类

`input_id` 是传输稳定 ID，即现行 SSE 帧的 `event_id`：有稳定 payload UUID 时取该 UUID，否则取 `csev_*` transport ID；重投复用同一 ID。

| `kind` | lane | 字段 | turn |
| --- | --- | --- | --- |
| `user.message` | task | `content`、`session_thread_id?` | 是 |
| `user.interrupt` | control | `session_thread_id?` | 否 |
| `permission.resolved` | control | `request_id`、`resolution` | 否 |
| `session.set_model` | control | `model` | 否 |

### 9.2 输入流与回执

- `GET /v1/dsr/sessions/{id}/inputs/stream`：SSE，帧为 `event: input`，`data` 为 `{"id","lane","kind",...}`；两路 consumer、Redis ACK 定位、`Last-Event-ID` 语义同现行。`keep_alive` 改为 SSE 注释行。
  - 后端只投递会话注册版本定义的 kind（4.1），所以收到未知 kind 属于协议缺陷。DSH 不执行、不停止会话，以 `processed` 加 `error: "unsupported_input"` 回执并记录 error 日志；后端发布 `session.error` 并计入指标，避免用户消息被静默吞掉。
- `POST /v1/dsr/sessions/{id}/inputs/ack`：`{"updates": [{"id", "status": "received"|"processing"|"processed", "error"?}]}`，语义同 [Worker Events Delivery](../ccrv2/ccr-v2-api-worker-events-delivery.md)，另加：
  - task 输入的 `processing` 在同一事务内把 turn 从 `accepted` 推进到 `processing`，并写入 `processed_at`。turn 已是 `processing` 时只更新 `worker_epoch`；**已是 terminal 时为 no-op 并计入 applied**，不回退状态。
  - task 输入的 `processed` 必须已有 terminal turn。批次中任一 task 输入不满足时，整批返回 `409 input_not_completed`，不执行任何 DoubleAck，错误消息给出第一个违规的 `id`。
  - `error` 只允许出现在 control lane 的 `processed` 上。
- `complete` 与 `ack processed` 保持两个请求：PostgreSQL 与 JetStream 无法合并为一个事务，现行 ACK 依赖 Code Session 锁、DoubleAck 与重投；分离可以让两者的责任清楚。

### 9.3 Turn 状态机与 `dsr_turns`

```text
dsr_turns
  id bigint generated always as identity
  uuid uuid default gen_random_uuid()
  organization_uuid, workspace_uuid, code_session_uuid, session_uuid uuid not null
  input_id text not null
  session_event_uuid uuid not null      -- 公开 user.message 行
  session_thread_id text not null
  state text not null                   -- accepted | processing | terminal
  outcome text null                     -- success | cancelled | failed | recovered
  reason text null                      -- 见 9.4
  request_sha256 bytea null
  worker_epoch bigint null              -- processing/terminal 时的 epoch
  accepted_at, processing_at, completed_at, acked_at timestamptz
  unique (code_session_uuid, input_id)
```

```mermaid
stateDiagram-v2
    [*] --> accepted: 公开 user.message 接纳（与 session_events 同事务）
    accepted --> processing: ack processing（当前 epoch）
    processing --> terminal: complete（当前 epoch）
    accepted --> accepted: 重投
    processing --> processing: 重复 processing / 接管后重投
    terminal --> terminal: 重复 complete 返回首次结果 / 重投后的 processing 为 no-op
```

- `accepted` 行在公开输入接纳事务内写入，因此**turn 与公开输入同生共死**。
- 首次 `complete` 之前必须先 `processing`；DSRv1 不保留"没有 processing 直接 processed"的旧兼容。终态之后的重投仍可回执 `processing`，按 9.2 为 no-op。
- **终态优先**：对 terminal turn 的任何重试，都先按幂等规则返回首次结果，不再检查 processing 前置条件。
- 接管不重置 turn：新 owner 收到重投后以新 epoch 再次回执 `processing`，`worker_epoch` 更新为新值。
- terminal 行永久保留到会话删除或保留期清理，用于幂等与接管补 ACK；跨进程按 `(code_session_uuid, input_id)` 查询。
- `acked_at` 由 `ack processed` 在 DoubleAck 成功的同一事务内写入，只用于领取资格（4.3）与诊断，不参与 reducer。DoubleAck 成功但事务提交失败时，`acked_at` 为空，会话可能被多领取一次，新 owner 的补 ACK 被 JetStream 忽略，结果正确。
- DSH 现行"已完成输入的重投直接补发事件并 ACK"的恢复分支不能机械迁移：DSRv1 中该分支依次执行 `processing`（no-op）、补发输出、`complete`（返回首次结果）、`ack processed`。

### 9.4 Complete

`POST /v1/dsr/sessions/{id}/inputs/{input_id}/complete`：

```json
{ "outcome": "success" | "cancelled" | "failed" | "recovered", "reason": "turn_failed" | "worker_shutdown" | "interrupted_recovery" | "quota_exhausted" }
```

- **不接受自由文本**。`reason` 仅在 `failed`/`recovered` 时必填，服务端映射为固定的公开文案（与现行 `turnOutcome` 的四种文案一致）以及现有安全错误分类，避免把 provider 错误、凭据或堆栈带入公开事件。
- 事务，锁顺序同输入接纳（公开 Session → Code Session）：
  1. 校验 token epoch。
  2. 校验 turn 存在且属于该 Code Session 与租户；否则 `409 input_not_eligible`。
  3. 已 terminal：请求摘要相同返回首次结果（即使由更早的 epoch 写入），不同返回 `409 turn_idempotency_mismatch`。此步先于第 4 步。
  4. 非 terminal 时必须处于 `processing`；否则 `409 input_not_eligible`。
  5. 写 terminal，按 7.6 关闭该输入的 pending 审批。
  6. 写入公开 `system.message`（failed/recovered）与 `session.error`，事件 ID 由 `input_id` 与 outcome 确定性派生。
  7. 按 10.3 计算并写入状态事件。
- 响应 `200 {"outcome", "public_status"}`。
- DSH 在本地 trajectory 中保留 `plugin:oma/input-completion`，用它决定何时调用 `complete`；输入在子线程全部结束或被中断后才算整体完成，由 DSH 在调用前保证。

### 9.5 接管、重试与并发

| 场景 | 行为 |
| --- | --- |
| `complete` 成功，响应丢失或 ACK 失败 | 输入重投给后续 owner；新 owner 回执 `processing`（no-op），从原生日志识别已完成输入，补发输出（幂等），重调 `complete`（终态优先，返回首次结果），再 `ack processed`；不重做工具、不重复终态 |
| 等待审批时崩溃 | 租约过期后按 open turn 领取（4.3）；注册时 pending 变为 `superseded`；重投的输入由新 owner 恢复并以 `recovered` 结束 |
| `ack processed` 早于 `complete` | `409 input_not_completed` |
| `complete` 早于 `processing` | `409 input_not_eligible` |
| `complete` A 与接纳 B 并发 | 共享公开 Session 锁串行；reducer 在锁内看到 B 是否已接纳 |
| A failed，B 已排队 | 见 10.2 |
| 旧 epoch 迟到的 `complete` | `409 epoch_superseded` |
| 输入属于其他会话 | `409 input_not_eligible` |

## 10. 公开状态 reducer

### 10.1 原则

公开 Session 与线程状态由**一个** reducer 计算，在持有公开 Session 锁的事务内运行。它是现行 `internal/db/session_event_state.go` 规则的移植，输入来源从 worker 状态与 `external_metadata` 换成 `dsr_turns` 与 `dsr_permission_requests`。**不从零重新推导**：粘滞规则、排队抑制与时间戳对齐按现行实现逐条迁移，并以现有测试固定。

### 10.2 输入事实与输出

输入（按线程）：

- 该线程 accepted/processing 的 turn；
- 该线程 pending 审批（含子线程）；
- 预算暂停（`budget_reached`）状态；
- 最近一次 terminal turn 的 outcome；
- 子线程由 DSH 输出的 `session.thread_status_*`。

规则（每个线程独立计算，再聚合到 Session）：

| 优先级 | 条件 | 线程状态 |
| --- | --- | --- |
| 1 | 预算暂停 | `idle(budget_reached)`，粘滞，按现行规则解除 |
| 2 | 该线程存在 pending 审批 | `idle(requires_action)`，`event_ids` 取 pending 记录的 `public_event_id` |
| 3 | 该线程存在 accepted 或 processing 的 turn | `running` |
| 4 | 子线程由 DSH 报告为 running | `running` |
| 5 | 否则 | `idle`，stop reason 取最近 terminal：`success`/`cancelled` → `end_turn`，`failed`/`recovered` → `retries_exhausted` |

Session 聚合**沿用现行 `sessionStatusAfterThread` 的优先级**：任一线程 `running` → `running`；否则任一线程 `rescheduling` → `rescheduling`；否则 `idle`，原因按以下顺序取第一个命中：`budget_reached`、任一线程 `requires_action`、主线程的 idle 原因。因此一个子线程等待审批时，其他线程可以继续执行，Session 保持 `running`，与现行并行语义一致。输入准入不随之改变：只要任一线程存在 pending 审批，新的 `user.message` 仍按 10.5 被拒绝。主线程失败后子线程结束不覆盖失败 stop reason；`retries_exhausted` 的粘滞与时间戳规则按现行 `resolveIdleStopReasonTx` 迁移。

**失败后仍有排队输入**：保留现行行为。现行 `resolveIdleStopReasonTx` 对 `retries_exhausted` 分支先返回，不执行 `HasInputBlockingIdle`。因此 A 以 `failed`/`recovered` 结束时，即使 B 已排队，也照常发布 `session.error` 与 `idle(retries_exhausted)`；B 开始处理时再回到 `running`。时间戳对齐规则（取最近 turn start 与上一状态的较晚者）按现行实现迁移。只有 `success`/`cancelled` 的 idle 受排队输入抑制。表中优先级 3 因此只用于 `success`/`cancelled` 之后的判断。

### 10.3 触发点

reducer 只在以下事务内运行，每个触发点都在同一事务内写入状态事件：

| 触发 | 典型结果 |
| --- | --- |
| 公开 `user.message` 接纳 | idle → running |
| 审批创建为 pending | running → requires_action |
| 审批 resolved/cancelled/closed/superseded | requires_action → running 或 idle |
| `complete` | running → idle 或保持 running |
| 更高 epoch 注册 | 清理旧 pending 后重新计算 |
| DSH 输出子线程状态事件 | 聚合变化 |
| 预算暂停与解除 | 按现行 |

`PUT state` 不是触发点。

### 10.4 状态事件 ID

状态事件 ID 替代现行 `syncPublicSessionFromWorker` 与预算 idle 使用的随机 `ids.New("sevt_")`：

- **输入字段**：`code_session_id`、目标（`session` 或具体 `session_thread_id`）、完整公开事件类型（如 `session.status_idle`、`session.thread_status_idle`）、触发类型、触发 ID、`stop_reason.type`、`stop_reason.detail`，以及排序去重后的 `stop_reason.event_ids`。
- **编码**：固定域前缀 `oma.dsr.status.v1`，各字段按上述固定顺序以"长度前缀 + UTF-8 字节"拼接，`event_ids` 内部同样逐项长度前缀，取 SHA-256 后以 `sevt_` 加 base32 前 26 位表示。
- 同一触发改变多个线程时，目标与事件类型不同，ID 不冲突；同一触发的重试得到相同 ID，不追加事件。
- **等价判断比较完整可观察状态**：状态、`stop_reason.type`、`detail`，以及排序去重后的 `event_ids`，与现行 `session_event_state.go` 的比较一致。两个 pending 解决一个后，状态仍是 `requires_action`，但 `event_ids` 变了，必须写入新事件。

### 10.5 依赖迁移

| 现行读点 | DSRv1 来源 |
| --- | --- |
| `validateSessionInputBatch`：`worker_status == requires_action` 或 `external_metadata` pending 时拒绝 `user.message` | 该 Session 存在 pending 审批（任一线程）时拒绝；`budget_reached` 规则不变 |
| `pendingSessionInputTools` / `PendingToolEventIDs` | `dsr_permission_requests` 的 pending 行 |
| `prepareSessionRemovalTx`：`worker_turn_started` 阻止删除/归档 | 存在 processing 状态的 turn 时拒绝；accepted 未 processing 不阻止（与"接纳时清零标记"的现行效果一致） |
| sandbox lifecycle SQL：`worker_status IN ('idle','running','requires_action')` | `worker_status IN ('idle','running')` |
| `syncPublicSessionFromWorker` | 删除，由 10.3 替代 |
| webhook idle 映射与 `stop_reason.event_ids` | reducer 输出 |

`worker_turn_started` 与 `external_metadata` 的列在 P5 删除；在此之前对 dsr1 会话不读不写。

## 11. 运行时能力接口

以下接口语义不变，路径迁移到 `/v1/dsr/sessions/{id}/` 下并改用 session token：

| 现路径 | DSRv1 路径 |
| --- | --- |
| `POST /v2/ccr-sessions/{id}/sandbox/mcp` | `POST .../sandbox/mcp` |
| `/v2/ccr-sessions/{id}/mcp`、`/mcp/*` | `.../mcp`、`.../mcp/*` |
| `POST /v2/ccr-sessions/{id}/web/search`、`/web/fetch` | `.../web/search`、`.../web/fetch` |
| `GET .../worker/subagent-threads/{thread_id}` | `GET .../threads/{thread_id}` |
| worker 图片附件路由 | `.../attachments/*` |
| `POST .../worker/diagnostics` | `POST .../diagnostics` |
| `POST .../worker/otlp/v1/logs`、`/traces` | `POST .../otlp/v1/logs`、`/traces` |

沙箱侧 `/v1/code/upstreamproxy/*` 与 `/v1/code/sessions/{id}/sign-commit` 不在本协议范围。

## 12. 可观测与容量

指标与日志（扩展现有 `oma.brain_claim.*`，字段遵循 Go 日志规范，不记录 payload）：

- trajectory：追加延迟、批大小、`gap`/`conflict` 次数、DSH 端待确认积压。
- permission：创建结果分布（allow/deny/pending）、pending 时长、自动关闭原因（closed/superseded）。
- turn：`complete` outcome 与 reason 分布、`input_not_completed` 次数、processing 到 terminal 时长。
- 回填：会话状态分布、批次耗时、失败原因。
- reducer：每个触发点的状态转换次数。

容量：

- 单条 trajectory 事件 16 MiB，请求体 16 MiB，读取页 8 MiB；单会话 trajectory 无硬上限，按 internal events 现有保留规则治理。
- 追加、审批、`complete` 都锁 `code_sessions` 行，与现行 internal events 写入相同；审批与 `complete` 额外锁公开 Session，锁顺序固定为公开 Session → Code Session → 业务行。
- trajectory 原地存放在 internal events，不存在双份存储窗口。

## 13. 迁移与发布

开发分阶段与生产切流分开：开发可以多个 PR 合入，新接口在切流前不承接生产会话；切流按会话进行，一个会话任一时刻只允许一种协议写入。

### 13.1 开发阶段

| 阶段 | 内容 |
| --- | --- |
| P0 | sandbox 模式弃用。版本 N：`agent_mode: sandbox` 启动时输出弃用警告，行为不变。版本 N+1：删除 sandbox 分支，所有 `== "brain"` 判断变为无条件，`agent_mode` 字段被忽略并告警（不报错，避免旧部署起不来）。P0 前先核对配置模板、Runner、镜像与部署入口 |
| P1 | 后端状态来源替换：`dsr_turns`、`dsr_permission_requests`、reducer、输入拒绝与删除准入改读新表、确定性状态事件 ID；先只对 dsr1 会话生效 |
| P2 | 后端 DSR 接口：claim/register/heartbeat/state/context、inputs、trajectory、permissions（含同事务发布）、output、complete；协议围栏与两侧 claim 过滤；切换开关默认关闭 |
| P3 | 协议状态转换、静止检查、投递时补建 turn 与回填任务（13.2–13.5） |
| P4 | DSH `src/dsr/` 客户端，按 trajectory 拆队列，完整替换 `src/ccrv2/`；同一构建只说一种协议 |
| P5 | 切流（13.3），稳定后清理（第 14 节） |

P1 先于 P2：同步审批与 `complete` 依赖新的状态来源，反过来做会与旧 worker 状态机冲突。

### 13.2 会话协议状态与围栏

`metadata.config.worker_protocol` 取值 `ccrv2`（含缺省）、`migrating`、`dsr1`。转换在锁定公开 Session 与 Code Session 的事务内进行：

```mermaid
stateDiagram-v2
    ccrv2 --> migrating: 静止检查通过，epoch + 1
    migrating --> dsr1: 回填完成，epoch + 1
    migrating --> ccrv2: 校验失败，epoch + 1
```

- **进入 `migrating` 时就推进 epoch**，而不是到 ready 才推进。此后旧 owner 的 token 全部失效。两种协议的 claim 都不选中该会话，两种协议的 register 都返回 `409 trajectory_migrating`，因此回填批次之间不会有旧 owner 重新注册写入。
- 每次状态转换都推进 epoch，任何一侧的旧 token 都不能跨越转换继续写入。
- `migrating` 期间公开输入照常接纳，并按目标协议写入 `dsr_turns`（13.4）；消息留在 JetStream 中，待 dsr1 owner 领取后投递。

### 13.3 切流步骤

前置条件：全部后端实例已升级到 P2 版本（旧后端的领取 SQL 没有协议过滤，存在旧后端时不得开启）；回填在目标环境完成演练。

1. 开启开关后，新建的 Code Session 直接写入 `worker_protocol = dsr1`，从第一条输入起就有 `dsr_turns`。
2. 存量会话**只在静止时**转换。静止的定义是在同一事务内检查以下条件：
   - 无有效租约；
   - 公开会话 idle，且不是 `requires_action` 或 `budget_reached`；
   - 不存在 `processed_at IS NULL` 的 `user.message`；
   - `external_metadata` 中没有待确认工具请求。
   - 进入 `migrating`（epoch 已推进，旧 owner 无法再拉取）后，该会话两个 durable consumer 的 `num_pending` 与 `num_ack_pending` 都为 0。`processed_at` 只表示已开始处理，不能证明已结算；"已完成但未 ACK"的旧输入只能通过 consumer 状态排除。检查不通过时转回 `ccrv2`。

   不满足的会话保持 `ccrv2`，由旧 DSH 继续服务，之后再试；始终不静止的会话可以一直留在旧协议上直到结束。
3. 旧 DSH 与新 DSH 并存，旧 DSH 只领取 `ccrv2` 会话。
4. **legacy 退出条件**：设定截止日期。届时仍为 `ccrv2` 的会话（回填失败或始终不静止）先按归档流程保存历史，再以 `session.error` 加 `session.status_terminated` 结束，并在发布说明中告知。P5 的可检查条件是"`worker_protocol` 为 `ccrv2` 的 active Code Session 数量为 0"，由指标给出。
5. 满足退出条件并稳定一个发布周期后，下线旧 DSH，进入第 14 节清理。

### 13.4 存量输入事实

静止检查保证转换时没有进行中的旧输入，但 JetStream 里仍可能有边界情况：静止检查之前接纳的输入在提交后才发布，或者旧 owner 已拉取但未 ACK。DSRv1 不依赖"转换瞬间队列为空"，而是在投递时补齐事实：

- **转换后接纳的输入**：接纳事务写入 `accepted` turn（13.2）。
- **投递时没有 turn 的 task 输入**：静止检查已经排除了这种情况，这里只是防御性兜底。DSR 输入流写出前，在事务内按其 `session_events` 行补建 turn：`processed_at IS NULL` 建为 `accepted`；`processed_at` 非空建为 `processing`，说明旧 owner 已开始处理，新 owner 收到后按恢复路径以 `recovered` 结束。补建按 `(code_session_uuid, input_id)` 唯一键幂等。
- **找不到 `session_events` 行的 task 输入**：视为协议错误，NAK 并告警，不投递。
- **转换前已完成的历史输入**：不建 turn。没有 turn 的历史输入不参与 reducer，也不影响删除准入。
- 旧 owner 在 `ccrv2` 时期写入 `external_metadata` 的待确认记录不迁移；静止检查已排除存在这类记录的会话。

### 13.5 回填

由于 trajectory 原地存放，回填只为已有 `dsh_log` 行补齐身份列并创建元数据行，不复制 payload。

- **执行位置**：后台 River 任务，不在 claim 或 register 请求路径上。
- **批次**：会话已是 `migrating`（13.2），每批一个事务，事务内校验状态与 epoch 未变，并写入 `migration_checkpoint`；崩溃后从检查点续跑，可重入。
- **读取**：通过"归档 + PostgreSQL"合并读回，并还原外置 payload。已归档段由 v2 codec 或幂等键重建身份（6.6）；从 `encodeURIComponent` 编码的幂等键解析 `dsh_session_id` 与 seq，Header 由 `:header` 后缀识别。子线程的 parent 取自 Header `parentSession`，公开线程等于 `dsh_session_id`（第 5 节），并核对现有子线程记录。
- **格式**：旧行 payload 为 `{type: "dsh_log", uuid, data}` 包装形式。回填不改写 payload，读取路径同时接受包装与原生两种形式，返回 `data` 或原生对象。
- **校验**：按 6.2 与 6.3 检查从 seq 0 起连续、身份一致、无冲突。历史上的保留与删除可能造成缺口，`last_internal_sequence_num` 也可能大于可见最大序号，这些都按校验失败处理。
- **失败**：转回 `ccrv2`（再推进一次 epoch），继续由旧 DSH 服务直到会话结束，并记录告警；不以 `permanent` 公开失败，不拼接修复。
- 旧路径在全部会话转换且新路径确认可用后才停止写入；由于原地存放，不需要删除旧行。

## 14. 删除范围

| 删除 | 时机 |
| --- | --- |
| sandbox 分支与 `agent_mode` 语义 | P0（两个版本） |
| `/v1` 与 `/v2` 的 `session_ingress/*`、`diag_logs`、WebSocket 退役占位 | P0 版本 N+1 |
| `/v1/code/sessions/{id}/worker/*`、`/v2/sessions/{id}`、`/v2/ccr-sessions/*` | P5 |
| Claude Code 输出 schema 与 mapper 分支、`IsClaudeCodeTranscriptEvent` | P5 |
| `is_compaction` 过滤、按 agent_id 的子线程分页 | P5 |
| `external_metadata`、`requires_action_details`、`worker_turn_started`、60+10 秒租约参数 | P5，删除前用代码搜索确认读点已全部迁移 |
| DSH `src/ccrv2/` | P5 |
| `docs/design/be/ccrv2/` 中仅描述 Claude Code 客户端的文档 | P5；仍有效的传输设计保留并更新路径 |

## 15. 测试计划

按"先失败场景，后成功场景"组织。以下为新增验收项，尚未执行。

- **Claim 与 Register**
  - 失败：旧 claim 不选中 dsr1 会话、新 claim 不选中非 dsr1 或 `migrating` 会话；耗尽 hold 与 `FailPermanently` 行为不变；旧 epoch 注册被拒；协议 token 与路由不匹配被拒。
  - 成功：410 后同 epoch 重新 register 恢复，不全量恢复；接管注册关闭旧 epoch 的 pending，并发布 running/idle。
- **Trajectory**（Mapper SQL 与参数绑定单测；真实 PostgreSQL 验证 bytea、jsonb、nullable 与部分唯一索引）
  - 失败：Header 缺失、seq 缺口、同序号语义不同、Header id 或 parent 不符、parent 或线程不存在、重复键、单条超过 16 MiB、旧 epoch 追加、`migrating` 时读写。
  - 成功：从 seq 0 首次追加返回 `"1"`；重复追加幂等；`from_seq = 0` 返回 Header 与 seq 0；按字节分页；外置 payload 还原；归档后合并读回无缺口；主线程与子线程分队列并行追加。
  - 比较：`1`/`1.0`、指数、键序、转义、大整数的 TS/Go 共用样本；摘要不同但语义相同时不判冲突。
- **Permission**
  - 失败：`input_id` 不属于该会话或未 processing、线程不属于该输入、同 `request_id` 不同内容、取消先到后创建不重开、resolved 与 cancel 竞争只有一个生效、complete 后迟到的确认不执行工具、旧 epoch 迟到审批、事务失败时不出现公开 requires_action。
  - 成功：allow/deny/pending、custom 与外部执行先经策略、delegation 按 multiagent 配置、子线程终止拒绝；resolved 早于 202 仍完成等待；POST 响应丢失后重试取得终态；接管时旧 pending 被 superseded，新输入不再 400；中断与 allow 竞态的两种结果。
- **Turn 与 complete**
  - 失败：任意或其他会话的 `input_id`、未 processing 即 complete、同 outcome 不同 reason、`ack processed` 早于 complete、旧 epoch complete、自由文本被拒。
  - 成功：complete 成功但 ACK 失败后接管，不重做工具、不重复终态；complete A 与接纳 B 并发不覆盖 running；A failed 且 B 排队时继续处理 B。
- **Reducer**：以现行 `session_event_state.go` 测试为基线逐条迁移；增加子线程 pending 对 Session 与线程状态的不同影响、`budget_reached` 粘滞、`retries_exhausted` 不被子线程覆盖、确定性状态事件 ID 重试不重复。
- **删除与归档准入**：processing 未完成时拒绝，accepted 未处理时允许，与现行行为对照。
- **协议版本**：缺失或不支持的版本返回 `unsupported_protocol`；同一 epoch 内换版本被拒；后端不向会话投递其注册版本未定义的输入 kind；同时支持两个版本的后端按请求头正确分派。
- **Output**：未知类型 400、整批原子、重复 `id` 幂等、`preview.*` 映射进入既有预览路径、`tool.result` 映射、模型代理与 DSH 双写去重。
- **迁移**
  - 失败：进入 `migrating` 后旧 owner 的 register 与写入均被拒（批次之间也不例外）；非静止会话（租约有效、requires_action、有未处理输入或待确认记录）不转换；软删除缺口回退为 `ccrv2` 并推进 epoch；投递时找不到 `session_events` 行的输入被 NAK。
  - 成功：归档历史回填（v1 段按幂等键重建身份）；半途崩溃后从检查点续跑；`migrating` 期间接纳的输入在 dsr1 领取后投递且有 turn；转换前已拉取未 ACK 的输入在投递时补建为 `processing` 并以 `recovered` 结束；旧 DSH、新 DSH 与升级后端并存。
- **接管**：等待审批时 SIGKILL，公开会话为 `idle(requires_action)`，租约过期后被 open turn 条件领取，pending 变为 `superseded`，输入以 `recovered` 结束，新 `user.message` 不再被拒；terminal 输入重投时 `processing` 为 no-op，`complete` 返回首次结果。
- **子线程引导**：子线程 Header 在 `thread_created` 之前写入成功；`thread_created` 在 trajectory 不存在或 parent 不符时被拒；同一 trajectory 绑定不同线程被拒；线程状态事件重启补发后不重复。
- **归档保真**：归档后同序号重复追加被判为重复或冲突，而不是插入新行；v2 段还原后两列与原值一致；v1 段重建的 seq 与 payload 不符时还原失败。
- **取消墓碑**：缺少 `dsh_session_id` 或 `tool_use_id` 被拒；派生 ID 与路径不符被拒；墓碑后到的创建返回 `cancelled`。
- **原生格式**：DSH 升级后继续向旧版本 trajectory 追加；改写已确认 Header 被判为冲突。
- **DSH**：现有 `ccrv2.test.ts`、`session-reacquisition.test.ts`、`crash.test.ts`、`parallel-tools.test.ts`、`parallel-subagents.test.ts`、`model-replication.test.ts`、`input-recovery.test.ts` 的场景迁移到 DSR 客户端，覆盖 3.1 的全部屏障、A→B→A 接管与 SIGKILL 恢复。
- **集成**：需要真实验证时，按 verify-be Skill 选择 chat 域受影响的最小场景，以及 Transcript 归档、还原、物理删除场景；是否运行由用户决定。

### 15.1 冻结验收条件

以下是冻结前必须通过的场景，尚未执行：

- [ ] pending 审批时 SIGKILL，租约到期后无需用户操作即可接管；审批变为 `superseded`，输入以 `recovered` 结束，新输入不再被拒。
- [ ] `complete` 提交后分别丢失响应、丢失 ACK：公开会话已 idle 时仍按 `acked_at` 为空被重新领取，重投只补结算，不重做工具。
- [ ] legacy 会话中已排队、processing、已完成未 ACK 三类输入：静止检查拒绝转换，或转换后安全结算。
- [ ] 回填两个批次之间，旧 owner 尝试 register 和写入，全部被围栏拒绝。
- [ ] 记录不存在时先 cancel 再晚到 create：墓碑落库，不会重新打开审批；终态重试不因输入已结束被拒。
- [ ] 子线程首次创建时，不需要放松工具执行或公开输出的复制屏障。
- [ ] 归档、删除 PG 行、读取、重复追加、还原之后，trajectory 身份与 `next_seq` 保持一致；删除最后副本后返回 `trajectory_purged`。
- [ ] 一个触发改变多个线程时事件 ID 不冲突；pending 集合缩小但状态仍为 requires_action 时，照常发布状态变化。
- [ ] reducer 行为对照表覆盖失败后排队、并行审批（其他线程继续执行时 Session 为 running）、预算暂停与线程终止，并与现行测试一致。
- [ ] 原生格式版本策略（仅 v4）、超限事件的永久失败规则、legacy 截止日期与退出指标已确定。
- [ ] 接管时本地更长且已确认部分内容不同，仍按现行规则报错并保留本地文件。

## 16. 已决定

- 基于 CCRv2 传输内核演进，协议名 DSRv1，路径前缀 `/v1/dsr/`；收益定位为边界清晰、删除兼容代码与显式不变量。
- 执行屏障保持不变。
- claim 与 register 保持分离，context 独立读取；`worker_claim_expires_at` 保留 hold 职责；两侧 claim 都加协议过滤。
- epoch 只来自 session token。
- trajectory 原地存放在 `code_session_internal_events`，以 `dsh_session_id`/`dsh_seq` 与元数据表表达；seq 从 0 起，`next_seq`/`from_seq` 为字符串；比较按语义，摘要用 JCS。
- 审批 ID 确定性派生，按 `(dsh_session_id, tool_use_id)` 去重；持久化与公开事件同事务；终态不可逆；complete、中断、接管、子线程终止自动关闭。
- `dsr_turns` 作为 turn 事实来源；`complete` 只接受枚举 reason。
- 公开状态由移植自现行规则的单一 reducer 计算，状态事件 ID 确定性派生。
- 开发分阶段，状态来源替换先于同步审批；切流在后端全量升级后按会话进行；回填异步且失败回退。
- 存在 open turn 且租约过期时可被领取；等待审批时崩溃由系统主动恢复，不等待用户响应。
- terminal 优先：终态之后的 `processing` 为 no-op，`complete` 先返回首次结果，再检查前置条件。
- 进入 `migrating` 即推进 epoch 并拒绝两侧注册；存量会话只在静止时转换；没有 turn 的在途输入在投递时补建。
- 唯一索引不以 `deleted_at` 为条件；archive codec v2 保存身份列，v1 段按幂等键重建。
- 子线程 Header 不依赖公开线程，由 `thread_created` 绑定；线程事件 ID 确定性派生。
- 取消请求必须携带 `dsh_session_id` 与 `tool_use_id`；状态事件 ID 包含线程维度。
- 已确认的 trajectory 不可改写；DSRv1 只支持原生格式 v4，迁移后的接续写入一律拒绝，generation 化留给 DSRv2。
- 可领取条件同时覆盖 processing 未结算与 terminal 未 ACK 的 turn，`acked_at` 由 ACK 事务写入。
- 审批按 `request_id` 查找已有记录，先于新建前的归属校验。
- Session 聚合沿用现行优先级（running 优先），子线程等待审批时其他线程可继续执行。
- 失败后有排队输入时保留现行行为：照常发布 `idle(retries_exhausted)`。
- 状态事件 ID 的输入包含目标与完整事件类型，按固定域与长度前缀编码；等价判断比较完整可观察状态。
- 单条原生事件上限 15 MiB；超限事件已进入本地日志时永久失败。
- 区分"归档后删 PG 行"与"删除最后副本"，后者标记 `purged`。
- 静止检查包含 consumer 的 `num_pending` 与 `num_ack_pending`；legacy 会话设截止日期，退出条件可由指标检查。
- 原生 seq 保持 number，字符串只用于 DSR 自有 ID 与游标。
- 不设 `capabilities`：词汇由协议版本固定，版本在 register 时绑定到会话；词汇变化发布新版本，先全量升级后端再升级 DSH。

## 17. 产品与运维决定

第 6 版关闭了此前的全部待定问题。每条决定都给出理由与可撤回的边界。

| 问题 | 决定 | 理由 |
| --- | --- | --- |
| 等待审批时崩溃：主动撤回，还是保留审批等用户响应 | **主动撤回**（3.3、4.3）。新 owner 把 pending 标为 `superseded`，输入以 `recovered` 结束，公开会话收到固定恢复文案与 `idle(retries_exhausted)` | 恢复不重放未完成的调用，保留的审批即使被批准，工具也不会执行，界面上的审批请求实际已经失效。保留方案还需要另外设计"用户响应才触发领取"的路径，并且在用户响应之前持续拒绝新输入。主动撤回让状态与事实一致，用户可以立刻重新提问 |
| 中断时服务端是否自动关闭目标线程的 pending | **关闭**（7.6），终态为 `closed`。之后迟到的确认按现行"工具确认已过期"响应 | DSH 收到中断后本来就放弃等待，只是把清理交给可能丢失的 cancel 请求。由服务端在接纳中断的事务内关闭，不依赖执行端存活，也避免中断后会话停在 requires_action、新输入被拒。迟到确认沿用已有的过期响应，公开 API 不新增错误类型 |
| legacy 截止日期与通知 | 默认 **开启切换后 30 天**，可通过部署配置调整；通知方式为发布说明与部署文档，到期前 7 天起在后端启动日志与指标告警中提示剩余的 legacy 会话数 | OMA 是自托管部署，直接受影响的是部署运维者，而不是最终用户；平台没有逐客户通知的通道，也不应为此新建。30 天覆盖常见的长时间空闲会话，只有回填失败或始终不静止的会话会走到截止日 |
| `dsr_trajectories`、`dsr_turns`、`dsr_permission_requests` 是否需要 `external_id` | **不需要** | `external_id` 的作用是兼容 Anthropic API 的资源 ID。这三张表是执行端内部的元数据与状态，不通过任何公开 API 暴露，也不进入事件 payload；`uuid` 已满足稳定标识与跨表引用规则。日后若需要对外暴露，再通过迁移补列 |
| `session.set_model` 是否需要公开确认事件 | **不新增**，保持现行：成功只体现在后续模型请求中，失败时 DSH 以 `processed` + `error` 回执，后端发布 `session.error` | 现行没有确认事件，增加它属于公开 API 变更，超出本协议的兼容边界 |
| `limit_bytes` 默认值 | 默认 **4 MiB**，上限 8 MiB，保持 6.7 | 冷恢复逐页写入临时 JSONL 再回读校验，内存占用以单页为界，不需要整条日志常驻；4 MiB 让常见会话一到两页读完，单条超大事件按软预算例外处理 |

## 18. 修订记录

| 版本 | 变更 |
| --- | --- |
| 1 | 初稿 |
| 2 | seq 从 0 起；审批增加 `input_id` 与持久化状态机；输入增加持久化事实与 complete 事务；迁移改为全量后端升级后按会话切流 |
| 3 | 补充收益与成本定位及职责对照；claim 与 register 恢复分离，保留 `worker_claim_expires_at` hold 职责，明确 410 恢复与两侧协议过滤；`external_metadata` 依赖清单与有类型 worker state（保留 `model`、`trace_turns` 读回）；身份词汇表；trajectory 改为原地存放在 internal events，复用归档、blob 与保留机制，按 trajectory 拆批，语义比较与 JCS，序号用字符串，新增 `DSR-Next-Seq` 与独立错误码；审批 ID 确定性派生、同事务发布、接管/中断/子线程终止自动关闭；新增 `dsr_turns` 与显式 reducer（移植现行规则、线程维度、触发点、确定性状态事件 ID、依赖迁移）；complete 改为枚举 reason；output 独立 schema、`preview.*` 边界映射、`capabilities` 协商、双写者合同；可观测与容量；P0 两版本弃用；状态来源替换先于同步审批；回填为后台任务且失败回退 |
| 4 | 等待审批时崩溃的领取条件（open turn）；terminal 优先的重试与 no-op processing；协议状态 `migrating` 进入即推进 epoch、两侧注册拒绝；存量会话静止转换与投递时补建 turn；唯一索引去掉 `deleted_at` 条件、archive codec v2 与 v1 按幂等键重建；子线程 Header 不依赖公开线程、由 `thread_created` 绑定、线程事件 ID 确定性派生；取消墓碑必填身份字段；状态事件 ID 增加线程维度；新增 DSH 原生格式升级约束（6.9） |
| 5 | 可领取条件增加 terminal 未 ACK 的 turn（`acked_at`），并排除预算暂停与耗尽 hold；审批先按 `request_id` 查找已有记录再做归属校验，说明子线程多次委派时的输入关联与接管不继承；Session 聚合改为沿用现行 running 优先；失败后排队输入保留现行 `idle(retries_exhausted)`；状态事件 ID 增加目标与事件类型并定义编码，等价判断包含 `event_ids`；原生 seq 保持 number；原生格式固定 v4 并区分传输回填与原生迁移；单条事件 15 MiB、超限进入本地日志即永久失败；软页预算；归档中的 trajectory 定位与 `purged`；接管时保留前缀核对；静止检查加入 consumer 状态；legacy 截止日期与退出指标；新增 15.1 冻结验收条件 |
| 6 | 关闭全部待定问题：等待审批时崩溃主动撤回审批；中断由服务端关闭目标线程 pending；legacy 截止默认 30 天并经发布说明、启动日志与指标告警通知；三张内部表不设 `external_id`；`session.set_model` 不新增公开事件；`limit_bytes` 默认 4 MiB |
| 7 | 删除 register 响应中的 `capabilities`，原因是单个实例的回答无法代表滚动升级中的整个后端，且只覆盖后端到 DSH 一个方向；改为词汇由协议版本固定，版本在 register 时绑定到会话并在响应中回显，词汇变化发布新版本，按先后端、后 DSH 的顺序部署 |
| 7.1 | 新增 [接口参考](api-reference.md)，逐个说明请求、响应、状态码与示例；同步明确 `/output` 每批 1–200 条及响应 `{accepted, duplicates}`、`inputs/ack` 遇到 `input_not_completed` 时整批不执行、Header 写入的响应字段 |

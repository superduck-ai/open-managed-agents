# CCR v2 Worker Epoch 机制后端设计文档

本文记录 code-session worker ingress 的 epoch 所有权机制。目标是让同一个 code session 在任意时刻只有当前 epoch 的 worker 可以写入；旧 worker 一旦被新 register 抢占，后续写请求必须返回 `409 Conflict`。

## 1. 设计目标

CCR v2 下，一个 Claude Code worker 可能因为容器重启、transport 重建或 lease 过期后的重新调度而被替换。服务端需要提供一个强一致的所有权边界：

- epoch 是 code session 级别的单调小整数 counter。
- legacy worker register 递增该 code session 的 epoch；managed-agent 凭证在签发时绑定一个已预留 epoch，register 对该 epoch 幂等。
- worker 写请求必须携带 `worker_epoch`。
- 请求 epoch 不等于当前 epoch 时返回 `409 conflict_error`，让旧 worker 退出。
- PostgreSQL 是唯一强一致来源，不使用内存 counter。

## 2. 数据模型

目标资源是 `code_sessions`，也就是 `cse_*` code session，不是 public `sessions`。

新增字段通过 goose migration 添加，不修改已应用 migration，不添加外键：

```sql
alter table code_sessions
  add column current_worker_epoch bigint not null default 0,
  add column worker_lease_expires_at timestamptz,
  add column worker_registered_at timestamptz,
  add column worker_last_heartbeat_at timestamptz,
  add column worker_token_session_id text,
  add column worker_binding jsonb not null default '{}'::jsonb;

create index if not exists code_sessions_worker_lease_expiry_v1_idx
  on code_sessions (worker_lease_expires_at)
  where deleted_at is null and worker_lease_expires_at is not null;
```

字段语义：

- `current_worker_epoch`: per-code-session counter，初始为 `0`，第一次 register 返回 `1`；`0` 只表示未注册 sentinel，worker 写请求中的 `worker_epoch` 必须是 `1+`。
- `worker_lease_expires_at`: 当前 epoch worker 的 lease deadline，register/heartbeat 设置为 `now + 60s`。
- `worker_registered_at`: 当前 epoch 注册时间。
- `worker_last_heartbeat_at`: 最近一次当前 epoch heartbeat 时间。
- `worker_token_session_id`: JWT 或 worker token 中的 session id claim；简化 token 方案下写 code session id。
- `worker_binding`: 少量绑定元数据，例如 auth mode、subject、issuer、trusted device 标记；不要存原始 token。

不持久化 `worker_alive`。读时用 `worker_lease_expires_at > now()` 推导，避免冗余状态和 sweep 竞态。

## 3. 核心不变量

| # | 不变量 | 服务端要求 |
|---|---|---|
| 1 | epoch 是 per-code-session 的 | 不同 `cse_*` 独立计数，互不影响 |
| 2 | register 获得唯一 epoch | legacy register 在事务内执行 `current_worker_epoch + 1`；managed register 只能提交 JWT 中已预留的 epoch |
| 3 | epoch 单调递增 | 不回退，不使用 timestamp、UUID 或随机数 |
| 4 | 只有当前 epoch 可以写 | 不匹配返回 `409 conflict_error` |
| 5 | lease 过期不自动 bump | 只有下一次 register 才 bump |

## 4. Register

新 worker 通过以下入口注册并 bump epoch：

- `POST /v1/code/sessions/{code_session_id}/worker/register`

legacy 客户端使用 `RegisterCodeSessionWorker(ctx, codeSessionID, binding, leaseTTL)`，在单个 DB 事务中递增 epoch：

```go
tx := begin()
session := select code_sessions
  where external_id = $1 and deleted_at is null
  for update

nextEpoch := session.current_worker_epoch + 1

update code_sessions
set current_worker_epoch = nextEpoch,
    worker_lease_expires_at = now + leaseTTL,
    worker_registered_at = now,
    worker_last_heartbeat_at = null,
    worker_token_session_id = binding.TokenSessionID,
    worker_binding = bindingJSON,
    connection_status = 'connected',
    last_worker_connected_at = now,
    last_worker_activity_at = now,
    updated_at = now
where id = session.id

commit()
return nextEpoch
```

设计要点：

- 使用 `select ... for update` 锁定当前 `code_sessions` 行。
- 不使用全局锁或 advisory lock；行级锁足够让同一 code session 的 register 串行化。
- 不同 code session 可以并发 register。
- 不等待 lease 过期；任何合法新 register 都可以抢占并 bump epoch。
- 返回 `worker_epoch` 建议用 JSON string，实际值保持在 JS safe integer 范围内。

managed-agent session-ingress JWT 额外携带 `worker_epoch`。首次创建时直接把现有 `current_worker_epoch` 初始化为 `1` 并签发 epoch `1`；Sandbox 恢复时，凭证轮换 SQL 在同一更新中替换 OAuth hash、把 `current_worker_epoch` 加一并清空旧 lease，再把新的 current epoch 写入 JWT。带该 claim 的 register 使用 `RegisterCodeSessionWorkerAtEpoch()`：持有行锁时再次要求数据库 current epoch 等于 JWT claim，然后把 lease/binding 写到该确切 epoch。相同 JWT 重试 register 只刷新同一 epoch，不再次 bump；旧 managed-agent JWT 通常在入口鉴权返回 `401`，即使其请求恰好在轮换前通过入口校验，事务内 worker epoch 复核也会阻止它抢回所有权。旧版不含 `worker_epoch` 的 JWT 可继续使用 legacy register，但凭证轮换预留 epoch 并清空 lease 后，legacy register 不得继续递增越过该 fence。

## 5. Worker 写请求校验

以下 worker 写请求必须解析 `worker_epoch`：

- `PUT /worker`
- `POST /worker/events`
- `POST /worker/internal-events`
- `POST /worker/events/delivery`
- `POST /worker/diagnostics`
- `POST /worker/heartbeat`

错误语义：

- missing/invalid `worker_epoch`（含 `0`、负数、小数、非数字）: `400 invalid_request_error`
- unknown code session: `404 not_found_error`
- epoch mismatch: `409 conflict_error`
- auth failure: `401 authentication_error`

`POST /worker/otlp/metrics` 与 `POST /worker/otlp/logs` 是例外：environment-manager 会在 worker register 前通过标准 OTLP exporter 上报生命周期 telemetry，且该 exporter 只携带 session Authorization。缺少 epoch 的 OTLP 请求在确认 session 存在后可以接收，但不得刷新 worker activity 或 lease；如果请求携带 epoch，则仍执行当前 epoch 与 active lease 校验，stale epoch 返回 `409`，非法 epoch 返回 `400`。

`POST /worker/events/delivery` 除了校验当前 epoch，还要求被 ACK 的 inbound event 已经由同一 epoch 的 SSE stream 写出并标记为 `sent`；queued 事件、旧发送 epoch 事件或未知事件不会推进状态，只计入 delivery 响应的 `ignored`。

普通只读校验方法：

```go
ValidateCodeSessionWorkerEpoch(ctx, codeSessionID, epoch) error
```

它只读取当前 epoch，用于不落库的轻量端点或读路径的兼容校验。注意：这个方法不能作为真正写入事件或 ACK 状态的唯一保护，因为它和后续写入之间可能被新的 register 插队。

## 6. Worker output 的条件写入保护

仅在 HTTP handler 中先调用 `ValidateCodeSessionWorkerEpoch()` 不够。存在如下竞态：

```text
current_worker_epoch = 1

旧 worker A: Validate epoch=1 通过
新 worker B: register，锁行并 bump 到 epoch=2，提交
旧 worker A: 继续处理 output event
```

worker output 不再写入私有 outbound event log。`POST /worker/events` 在处理整批
output 前，先执行一次带 epoch 条件的 activity update：

```sql
update code_sessions
set last_worker_activity_at = $now,
    updated_at = $now
where external_id = $code_session_id
  and current_worker_epoch = $worker_epoch
  and deleted_at is null;
```

0 rows 时再区分 session 不存在与 epoch mismatch。该条件 update 是整个 batch 的
线性化点；它与 register 对同一 `code_sessions` 行的更新互斥：

- 如果旧 worker 的条件 update 先完成，则该 batch 视为在抢占前已被服务端接纳。
- 如果 register 先 bump epoch，则旧 worker 的条件 update 影响 0 rows，整个 batch 返回 `409`，不会发布 public event 或生成 control response。

当前 worker 写事件路径应调用带 epoch 的 service 入口：

```go
AppendWorkerOutputEventsForEpoch(ctx, codeSessionID, workerEpoch, events)
```

HTTP envelope 只把每个 `payload` 解码为 `json.RawMessage`。服务端先用公共 header
schema 读取 `type` 和 `uuid`，只有 `control_request` 等确实需要解释的类型才继续解析
具体 schema；未知类型与未知字段保持在原始 payload 中。事件 metadata 也通过命名
schema 读取公共字段，不使用 `map[string]any` 充当已知字段模型。只有公开事件映射需要
遍历、拆分或补充开放结构时，才会生成可变的公开 payload。公开事件 mapper 自身接收
`json.RawMessage`，按 `assistant`、`user`、`system`、`result` 或 opaque 类型解析对应 schema；
已知字段不再由调用方解码成 `map[string]any` 后传入。整批事件的 schema 解析、metadata
构建和公开 payload 转换均在 epoch gate 前完成；gate 通过后的 apply 阶段只执行权限处理
和事件发布，不再因后续事件格式错误造成同一 batch 部分发布。

单条 epoch 兼容入口同样先解析并准备事件，再执行带 epoch 条件的 activity update。
该条件更新既校验 worker 所有权，也刷新活跃时间；`keep_alive` 通过该更新后直接返回，
不会再执行一次无条件 heartbeat。无 epoch 的兼容入口则只在事件类型确实为 `keep_alive`
时执行普通 activity update。事件应用阶段不携带 heartbeat 策略参数。

batch 中可公开的 durable output 直接以稳定 public event ID 幂等写入
`session_events`；`ephemeral`、隐藏协议事件和 diagnostics 不持久化。工具权限的
`control_response` 仍写入 inbound 队列，并依靠由原始 `request_id` 派生的稳定 UUID
去重。internal transcript 仍按原有事务在 code session 行锁内校验 epoch 后写入
`code_session_internal_events`。

其中 ephemeral `stream_event` 会在 epoch gate 之后通过 Core NATS 实时扇出到各
API 实例，但仍不落 PostgreSQL；协议转换、稳定 ID 与故障语义见
[`ccr-v2-worker-sse-fanout.md`](ccr-v2-worker-sse-fanout.md)。

legacy ingress 路径如果继续使用无 epoch 的 `AppendWorkerEvent()`，必须被明确归类为兼容路径；如果要纳入 CCR v2 所有权保证，也需要改造成 register/epoch 模型。

## 7. Heartbeat 与 Lease

lease TTL 固定默认 `60s`。heartbeat 只续当前 epoch，不 bump epoch：

```sql
update code_sessions
set worker_last_heartbeat_at = now(),
    worker_lease_expires_at = now() + ttl,
    last_worker_activity_at = now(),
    connection_status = 'connected',
    updated_at = now()
where external_id = $1
  and current_worker_epoch = $2
  and deleted_at is null
returning worker_lease_expires_at;
```

0 rows 时再查 session 是否存在：

- 不存在，返回 `ErrNotFound`，HTTP `404`。
- 存在但 epoch 不匹配，返回 `ErrWorkerEpochMismatch`，HTTP `409`。

lease 过期本身不修改 epoch。UI 或观测状态用 `worker_lease_expires_at <= now()` 判断 stale。后台 sweep 在 v1 中不是正确性的必要条件。

同一个 Provider Sandbox 因 idle timeout 被 pause 时，worker ownership 没有变化，因此 resume 也不 bump epoch。用户事件触发显式 `Connect`/`SetTimeout` 并确认同一个活动 Sandbox 恢复成功后，服务端可以把当前已注册 lease 重新设为 `now + leaseTTL`。该写入必须同时匹配租户、Code Session、活动 Work 和精确的 Provider Sandbox ID，并拒绝 lease 已被 credential rotation 清空的记录；Provider 恢复失败时不得续租。replacement 创建新 Sandbox 时仍必须 bump epoch 并清空旧 lease。

## 8. 读路径兼容

读路径不 bump epoch：

- `GET /worker` 可以可选支持 `worker_epoch` query/header；传了且不匹配返回 `409`，没传则返回当前 worker state。
- `GET /worker/events/stream` 优先使用 query/header 中的 epoch；没有显式参数时使用已认证 JWT 的正 `worker_epoch`。显式值不匹配返回 `409`；只有旧 JWT 也没有 epoch 时才保持 legacy 兼容。
- SSE/HTTP poll 读取 queued inbound events 不改变 epoch。

当前实现约束：

- 无 `worker_epoch` 的 `GET /worker` 只读当前 state，不刷新 `connection_status` 或 activity 时间。
- 带 `worker_epoch` 的 `GET /worker` 可以刷新连接状态，但必须使用 `current_worker_epoch = $epoch` 条件更新。
- `GET /worker/events/stream` 在请求参数或 JWT claim 提供当前 epoch 时写入 connected/disconnected 状态；只有两者都无 epoch 时才不做状态写入。
- 带 `worker_epoch` 的 stream 会先解析 `from_sequence_num` / `Last-Event-ID`；cursor 非法时返回 `400`，不会先把 worker 标记为 connected。
- 带 `worker_epoch` 的 stream 使用 `ListCodeSessionInboundEventsForWorkerStream(ctx, sessionID, epoch, afterSequence)`，读取 `sequence_num > afterSequence` 且 `delivery_status <> 'processed'` 的 inbound events，并继续约束 `current_worker_epoch = $epoch`。
- epoch-scoped stream 写出事件后使用 `MarkCodeSessionInboundEventSentForEpoch()` 标记 `sent`、`delivery_worker_epoch` 和 delivery attempt；新 register bump 后，旧 stream 应停止，不能读取或标记新 epoch 的事件。
- 请求参数和 JWT claim 都无 `worker_epoch` 的 legacy stream 仍使用 queued-only 兼容路径，不提供 epoch takeover/replay 语义。managed-agent JWT 带 epoch，因此它即使使用不带 query 的 stream URL，也必须进入 epoch-scoped 路径并在 pause/resume 重连时重放未 processed 的事件。
- stream 断开时旧 epoch 的 disconnected 更新会被条件 update 拒绝，不能覆盖新 worker 的 connected 状态。

## 9. 并发与竞态防护

| 风险 | 防护 |
|---|---|
| 两个 register 并发 | `RegisterCodeSessionWorker` 在同一 code session 行上 `for update`，返回连续不同 epoch |
| 不同 session 并发 | 行级锁互不阻塞 |
| 旧 worker 预校验后被抢占 | output batch 先执行带 `current_worker_epoch` 条件的 activity update；internal event 在 append 事务内校验 epoch |
| heartbeat 旧 epoch 续租 | 条件 update 带 `current_worker_epoch = $epoch` |
| lease 过期和 register 竞态 | lease 过期不改 epoch，register 行锁负责所有权切换 |
| idempotency key 重放 | public event 使用稳定 ID 幂等插入；control response 使用 request 派生 UUID；internal event 在事务内查重 |
| 读路径状态写覆盖当前 worker | 只有携带当前 epoch 的读路径才允许条件刷新 connected/disconnected |

## 10. HTTP 契约

`GET/PUT /worker` 的 worker state patch/read 契约记录在
`docs/design/ccr-v2-api-worker-state.md`。本节只保留 epoch ownership
和 register 的跨接口语义。

`POST /worker/register` 返回：

```json
{
  "worker_epoch": "1"
}
```

## 11. Legacy 兼容边界

legacy WebSocket transport 已移除，包括：

- `/v1/session_ingress/ws/{code_session_id}`
- `/v2/session_ingress/ws/{code_session_id}`
- `/v1/code/sessions/{code_session_id}` 的 WebSocket upgrade 行为

前两个显式 WebSocket 路径不存在，canonical code session 路径收到 WebSocket upgrade 请求时返回 `404`；不带 upgrade 的 `GET` 仍保持 HTTP poll 兼容语义。

CCR v2 worker 使用持久化 inbound event 队列和带 epoch 的
`/v1/code/sessions/{code_session_id}/worker/events/stream`，不依赖进程内 WebSocket worker 连接。

以下 legacy HTTP 入口仍保留“不要求 worker epoch”的行为，用于兼容旧 session ingress 客户端，但鉴权同样要求有效的 `sk-ant-si-<JWT>`，不再接受原始 `cse_...` token；这些入口不属于 CCR v2 worker epoch 所有权面：

- `/v1/session_ingress/session/{code_session_id}`
- `/v2/session_ingress/session/{code_session_id}`
- 上述路径的 `/events`、`/diag_logs` 子资源
- `/v1/code/sessions/{code_session_id}` HTTP poll / persistence 兼容入口
- `/v2/sessions/{code_session_id}` session context 兼容入口

这些入口仍可写 worker event，但不提供“旧 worker 被新 register 抢占后必然 409”的语义。要让它们进入 CCR v2 所有权保证，需要先定义对应客户端 register/epoch 传递契约，再改成 `AppendWorkerEventForEpoch()`。

## 12. 测试要求

需要覆盖：

- legacy code session 第一次 register 返回 `1`，第二次返回 `2`。
- managed-agent 新 JWT 重复 register 返回相同的预留 epoch；恢复轮换后旧 JWT 返回 `401`，新 JWT 返回下一 epoch。
- 同一 code session 两个并发 register 返回连续不同 epoch。
- 不同 code session 都从 `1` 开始，互不影响。
- 旧 epoch 调 worker 写 endpoints 返回 `409`。
- 当前 epoch 调同样 endpoints 成功。
- 必须携带 epoch 的 worker 写入口缺失、`0`、非数字、浮点、负数 `worker_epoch` 返回 `400`；OTLP 缺失 epoch 可以成功且不更新 worker activity/lease，非法 epoch 仍返回 `400`。
- heartbeat 当前 epoch 更新 heartbeat/lease 时间。
- heartbeat 旧 epoch 返回 `409` 且不更新 lease。
- lease 过期不自动 bump，下一次 register 才 bump。
- 事务内保护回归：旧 epoch 已经通过 HTTP 预校验后，若新 register 先 bump，旧 epoch append event 仍必须返回 `ErrWorkerEpochMismatch` 且不插入事件。
- 状态写入回归：无 epoch 的读路径不刷新 `connection_status`；旧 epoch 的 connected/disconnected 更新不能覆盖当前 worker 状态。
- SSE stream 回归：带旧 epoch 的 stream 在新 register 后停止，且不能消费或标记新 epoch 之后的 queued inbound event。
- SSE stream cursor 回归：非法 `from_sequence_num` / `Last-Event-ID` 返回 `400`，且不会刷新 connected 状态。
- delivery ACK 回归：当前 epoch 的 `/worker/events/delivery` 可以推进已由当前 epoch stream 标记为 `sent` 的事件；旧 epoch 返回 `409`；未知、未发送或 stale epoch event 计入 `ignored`。

相关测试命令：

```bash
go test ./tests -run 'TestCodeSessionWorker' -count=1 -v
go test ./internal/db ./internal/codesessions -count=1
```

## 13. 实现 Checklist

- [x] `current_worker_epoch` 存在于 `code_sessions`，初始 0。
- [x] register 通过唯一的 DB 方法 bump epoch。
- [x] register 的 bump、lease、binding 更新在同一事务和同一行锁内完成。
- [x] worker 写请求解析并校验 `worker_epoch`。
- [x] 落库 worker event 的 append 事务内再次检查 epoch。
- [x] 旧 epoch append 不查 idempotency、不 insert event、不更新 sequence。
- [x] heartbeat 使用带 epoch 条件 update 续租。
- [x] worker connected/disconnected/activity 状态写入提供 epoch-scoped 版本。
- [x] 无 epoch 的 `/worker` 读路径不刷新 worker 状态。
- [x] epoch-scoped SSE stream 支持 `from_sequence_num` / `Last-Event-ID` cursor，并按未 `processed` 事件重放。
- [x] epoch-scoped SSE 写出后使用当前 epoch 标记 `sent` 和 delivery attempt。
- [x] `/worker/events/delivery` 解析 ACK updates，并在当前 epoch 事务内单调推进 delivery 状态。
- [x] 不持久化 `worker_alive`，读时由 lease deadline 推导。
- [x] `worker_binding` 不存原始 token。
- [x] epoch 返回值兼容 JSON string/number，保持 JS safe integer 范围。

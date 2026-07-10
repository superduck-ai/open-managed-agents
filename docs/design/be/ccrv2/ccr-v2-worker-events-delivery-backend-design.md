# CCR v2 Worker Events Delivery ACK 后端实现设计

> 本文记录 `POST /v1/code/sessions/{session_id}/worker/events/delivery` 的当前后端实现：API 契约、请求 body 处理、ACK 落库规则、没有 `received` 时的语义，以及服务端重发路径。
>
> 相关契约文档：[`ccr-v2-api-worker-events-delivery.md`](ccr-v2-api-worker-events-delivery.md)。
> epoch 语义：[`ccr-v2-epoch-design.md`](ccr-v2-epoch-design.md)。

---

## 1. 结论

`/worker/events/delivery` 不是 worker 输出事件接口，而是 worker 对服务端通过 SSE 推送的 `client_event` 做**应用层 ACK**。

服务端不能把 SSE 写出成功当成 worker 已收到。当前实现区分以下状态：

| 状态 | 含义 |
|---|---|
| `queued` | 事件已入库，尚未尝试写给 worker |
| `sent` | 服务端已把 SSE frame 写出，但 worker 尚未 ACK |
| `received` | worker SSETransport 已收到 client_event |
| `processing` | worker 已消费该命令并开始处理 |
| `processed` | worker 已处理完成，后续不应重发 |

当前后端实现已经落地：

- delivery handler 解析并校验 `updates`。
- ACK 在事务内校验当前 `worker_epoch`。
- ACK 只接受已经由当前 epoch stream 写出并标记为 `sent` 的事件。
- 状态按 `sent < received < processing < processed` 单调推进。
- unknown、queued、旧 epoch 或未被当前 epoch 发送过的 ACK 不报错，计入 `ignored`。
- 成功响应返回 `ok/applied/ignored`。

---

## 2. API 契约

### 2.1 Endpoint

| 项 | 值 |
|---|---|
| Method | `POST` |
| URL | `/v1/code/sessions/{session_id}/worker/events/delivery` |
| 方向 | worker -> server |
| 调用方 | Claude Code `CCRClient.reportDelivery()` |
| 鉴权 | `Authorization: Bearer <session_access_token>` |

### 2.2 Headers

| Header | 必填 | 说明 |
|---|---|---|
| `Authorization` | 是 | `Bearer <session_access_token>` |
| `Content-Type` | 是 | `application/json` |
| `anthropic-version` | 是 | `2023-06-01` |
| `User-Agent` | 否 | Claude Code 客户端会带 |

### 2.3 Body

```jsonc
{
  "worker_epoch": 1,
  "updates": [
    {
      "event_id": "5e34a4de-456f-4bb5-ba2c-152cf71d3fa1",
      "status": "processing"
    }
  ]
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `worker_epoch` | integer 或 string integer | 是 | 当前 worker epoch；必须是正 int64；`0` 是未注册 sentinel，不合法 |
| `updates` | array | 是 | 批量 ACK，长度 `1..64` |
| `updates[].event_id` | string | 是 | 被 ACK 的 `client_event` ID |
| `updates[].status` | enum | 是 | `received`、`processing`、`processed` |

> Claude Code 客户端对象内部可能以 `0` 初始化 epoch，但实际写请求必须先通过 `/worker/register` 或 `/bridge` 获得 `1+` 的 epoch。服务端 HTTP parser 和 DB ownership helper 都拒绝 `0`。

### 2.4 Response

成功响应：

```json
{
  "ok": true,
  "applied": 1,
  "ignored": 0
}
```

| 状态码 | 语义 |
|---|---|
| `200` | 请求已处理；部分 update 可能被计入 `ignored` |
| `400` | JSON、`worker_epoch`、`updates` 或 `status` 非法 |
| `401` / `403` | 鉴权失败 |
| `404` | code session 不存在 |
| `409` | `worker_epoch` 与当前 session epoch 不一致 |
| `429` | 限流；客户端会读取 `Retry-After` |
| `5xx` | 服务端错误；客户端会无限重试 |

客户端不消费响应体，只看 HTTP status。未知 `event_id`、queued 事件 ACK、旧发送 epoch ACK 不返回 4xx/5xx，否则 Claude Code 的 delivery uploader 会无限重试并堵住 ACK 队列。

---

## 3. 请求 Body 处理

handler 路径：

```text
handleCodeSessionWorkerDelivery
  -> requireWorkerEpochBody
  -> decodeCodeSessionWorkerDeliveryPayload
  -> DB.ApplyCodeSessionWorkerDeliveryUpdates
```

解析规则：

1. body 必须是 JSON object。
2. `worker_epoch` 必须存在；接受 JSON integer 或 string integer。
3. `worker_epoch` 必须是正 int64；`0`、负数、小数、非数字字符串返回 `400`。
4. `updates` 必须存在，必须是非空数组。
5. `updates` 长度最多 64。
6. 每个 update 的 `event_id` trim 后必须非空。
7. 每个 update 的 `status` trim 后必须是 `received | processing | processed`。
8. `sent` 不是 worker ACK 状态，delivery body 中出现 `sent` 会返回 `400`。

例如请求：

```json
{
  "worker_epoch": 1,
  "updates": [
    {
      "event_id": "5e34a4de-456f-4bb5-ba2c-152cf71d3fa1",
      "status": "processing"
    }
  ]
}
```

当前 epoch 匹配且该事件已经由 epoch 1 的 SSE stream 标记为 `sent` 时，服务端会：

- 将状态至少推进到 `processing`。
- 补齐 `received_at` 和 `processing_at`。
- 设置 `last_delivery_update_at`。
- 返回 `applied = 1`。

如果事件不存在、仍是 `queued`、或曾由旧 epoch 标记为 `sent`，服务端返回 `200`，但该 update 计入 `ignored`。

---

## 4. event_id 关联规则

服务端推给 worker 的 SSE envelope 必须保证：

```text
StreamClientEvent.event_id = payload.uuid
```

原因：

| ACK 类型 | TS 端 event_id 来源 |
|---|---|
| `received` | SSE frame 的 `event.event_id` |
| `processing` / `processed` | command lifecycle 的 `uuid` |

如果 SSE `event_id` 使用内部行 ID（例如 `csev_...`），而 lifecycle 使用 payload `uuid`，服务端会看到两条互不相关的状态：`received(csev_...)` 和 `processed(uuid)`。

当前后端规则：

1. canonical event id 使用 `code_session_inbound_events.payload_uuid`。
2. `writeCodeSessionWorkerSSEEvent()` 输出 envelope 时，`event_id` 优先取 `payload_uuid`。
3. 如果历史/异常事件没有 `payload_uuid`，fallback 到内部 `external_id`。
4. delivery ACK 落库查找时，先按 `payload_uuid = event_id`，找不到再按 `external_id = event_id` 兼容。

---

## 5. 数据模型

delivery ACK 字段通过 goose migration `internal/db/migrations/00007_add_code_session_inbound_delivery_ack.sql` 添加，不修改已应用 migration。

```sql
alter table code_session_inbound_events
  add column if not exists delivery_worker_epoch bigint,
  add column if not exists received_at timestamptz,
  add column if not exists processing_at timestamptz,
  add column if not exists processed_at timestamptz,
  add column if not exists last_delivery_attempt_at timestamptz,
  add column if not exists last_delivery_update_at timestamptz,
  add column if not exists delivery_attempts integer not null default 0;

create index if not exists code_session_inbound_events_payload_uuid_v1_idx
  on code_session_inbound_events (code_session_id, payload_uuid, sequence_num asc)
  where deleted_at is null and payload_uuid is not null;

create index if not exists code_session_inbound_events_unprocessed_v1_idx
  on code_session_inbound_events (code_session_external_id, sequence_num asc)
  where deleted_at is null and delivery_status <> 'processed';
```

字段语义：

| 字段 | 语义 |
|---|---|
| `delivery_worker_epoch` | 最近一次成功写出/ACK 对应的 worker epoch |
| `sent_at` | 第一次标记为 sent 的时间 |
| `received_at` | 第一次收到 received 或更高状态 ACK 的时间 |
| `processing_at` | 第一次收到 processing 或 processed ACK 的时间 |
| `processed_at` | 第一次收到 processed ACK 的时间 |
| `last_delivery_attempt_at` | 最近一次 SSE 写出并标记 sent 的时间 |
| `last_delivery_update_at` | 最近一次接受 delivery ACK 的时间 |
| `delivery_attempts` | SSE 写出尝试计数 |

`delivery_status` 允许值：

```text
queued | sent | received | processing | processed
```

---

## 6. Delivery Handler 落库流程

### 6.1 epoch 校验

`ApplyCodeSessionWorkerDeliveryUpdates(ctx, sessionID, epoch, updates)` 在 DB transaction 内完成：

1. `epoch <= 0` 直接返回 epoch mismatch，HTTP 层映射为非法或冲突。
2. `select code_sessions ... for update` 锁定当前 code session。
3. session 不存在返回 `404`。
4. request `worker_epoch != current_worker_epoch` 返回 `409`。
5. epoch 匹配才继续批量处理 ACK。

### 6.2 ACK 接受门槛

每个 update 会先查事件：

1. 按 `payload_uuid = event_id` 查找，按 `sequence_num asc` 取第一条并 `for update`。
2. 找不到则按内部 `external_id = event_id` fallback。
3. 仍找不到则 `ignored++`。

匹配到事件后，只有同时满足以下条件才会应用 ACK：

```text
event.delivery_worker_epoch == request.worker_epoch
and delivery_status >= sent
```

因此：

| 场景 | 当前行为 |
|---|---|
| event 不存在 | `ignored++` |
| event 仍是 `queued`，还没被当前 stream 写出 | `ignored++` |
| event 是旧 epoch 写出的 `sent/received/processing` | `ignored++` |
| event 已被当前 epoch 写出并标记 `sent` 或更高 | 应用 ACK |

这个门槛避免 worker 伪造或误报未投递事件，也避免旧 worker 的迟到 ACK 推进当前 epoch 的事件状态。

### 6.3 状态推进

状态顺序：

```text
sent < received < processing < processed
```

更新规则：

| 收到 status | 后端动作 |
|---|---|
| `received` | 设置 `received_at = coalesce(received_at, now())`，状态至少推进到 `received` |
| `processing` | 隐含 `received`，设置 `received_at` 和 `processing_at`，状态至少推进到 `processing` |
| `processed` | 隐含 `received + processing`，设置三类时间戳，状态推进到 `processed` |

同一 epoch 内状态只能单调前进。低状态 ACK 晚到时不会回退 `delivery_status`，但只要通过接受门槛，仍计入 `applied` 并刷新 `last_delivery_update_at`。

成功应用时：

```text
applied += 1
delivery_worker_epoch = request.worker_epoch
last_delivery_update_at = now()
code_sessions.last_worker_activity_at = now()
```

delivery ACK 不续租 `worker_lease_expires_at`；续租仍只由 heartbeat 负责。

---

## 7. 没有 received 时怎么处理

如果服务端写出 SSE 后没有收到 `received`：

1. 事件保持 `delivery_status = sent`。
2. 不把它当成完成，也不立即在同一条健康 SSE 连接上重复发送。
3. 如果后续收到 `processing` 或 `processed`，反推该事件已 `received` 并补齐 `received_at`。
4. 当前实现未加入专门的 lag metric；后续可以记录 `sent` 后长时间无 ACK 的 log/metric。
5. lease 级别的不健康判断仍交给 heartbeat/lease；delivery ACK 本身不续租。

核心原则：

```text
sent != received
没有 received = 尚未被 worker 应用层确认
但不能在健康连接内无脑重发，避免制造重复 prompt
```

---

## 8. 服务端如何重发

重发不通过 delivery 端点。delivery 只是 ACK 写入口。

服务端重发发生在：

```http
GET /v1/code/sessions/{session_id}/worker/events/stream
```

也就是重新把 `code_session_inbound_events` 包装成 `event: client_event` SSE frame 写给 worker。

### 8.1 SSE frame

```text
id: 12
event: client_event
data: {
  "event_id": "5e34a4de-456f-4bb5-ba2c-152cf71d3fa1",
  "sequence_num": 12,
  "event_type": "user",
  "payload": { ... }
}
```

`event_id` 必须稳定，重发时不能生成新的 ID。当前实现的 SSE `id` 使用 `sequence_num`，body 里的 `event_id` 优先使用 `payload_uuid`。

### 8.2 stream 建连与 cursor

`handleCodeSessionWorkerEventsStream` 当前顺序：

1. ingress token 鉴权。
2. 确认 code session 存在。
3. 可选解析并校验 `worker_epoch` query/header。
4. 解析 `from_sequence_num` 或 `Last-Event-ID`。
5. cursor 非法时直接返回 `400`，不会把 worker 标记为 connected。
6. 如果带当前 epoch，调用 `MarkCodeSessionWorkerConnectedForEpoch`。
7. 写 SSE headers，进入 polling loop。
8. stream 退出时用 epoch 条件调用 `MarkCodeSessionWorkerDisconnectedForEpoch`。

无 epoch 的 stream 保持兼容，只读 queued events，不刷新连接状态。

### 8.3 写出后的状态

epoch-scoped SSE 写出成功后调用 `MarkCodeSessionInboundEventSentForEpoch`：

```text
delivery_status = case when queued then sent else delivery_status end
sent_at = coalesce(sent_at, now())
delivery_worker_epoch = current_worker_epoch
last_delivery_attempt_at = now()
delivery_attempts = delivery_attempts + 1
```

不得标记为 `received` 或 `processed`。

注意当前顺序是**先写 SSE frame，再标记 sent**。如果写出后 mark 失败，stream 会继续处理；若失败原因是 epoch mismatch，旧 stream 直接停止。

### 8.4 同一 epoch 普通重连

Claude Code SSETransport 会发送：

```text
from_sequence_num=<lastSequenceNum>
Last-Event-ID: <lastSequenceNum>
```

epoch-scoped stream 使用本地 `lastSentSequence` 从 cursor 开始推进，每个 polling loop 只查询并写出 `sequence_num > lastSentSequence` 的事件，避免同一条健康连接反复发送同一事件。

查询约束：

```sql
select e.*
from code_session_inbound_events e
join code_sessions cs on cs.id = e.code_session_id
where e.code_session_external_id = $1
  and e.sequence_num > $3
  and e.delivery_status <> 'processed'
  and not (
    e.delivery_status = 'sent'
    and e.delivery_worker_epoch is null
    and e.received_at is null
    and e.processing_at is null
    and e.processed_at is null
  )
  and e.deleted_at is null
  and cs.deleted_at is null
  and cs.current_worker_epoch = $2
  and cs.current_worker_epoch > 0
order by e.sequence_num asc;
```

如果查询返回 0 行，DB 层仍会再次校验当前 epoch；新 register bump 后，旧 stream 会停止。

### 8.5 新 epoch 接管

新 worker register 后，`current_worker_epoch` 增加。新 epoch 的 SSE stream 会重放未 `processed` 且满足查询条件的事件：

| 状态 | 是否重放 |
|---|---|
| `queued` | 是 |
| `sent` | 是，但排除 legacy sent/null epoch/no ACK 形态 |
| `received` | 是 |
| `processing` | 是 |
| `processed` | 否 |

当前实现没有单独的 delivery cutover marker；它直接排除以下 legacy 形态：

```text
delivery_status = 'sent'
and delivery_worker_epoch is null
and received_at is null
and processing_at is null
and processed_at is null
```

这能避免升级后把旧实现中已 `sent`、但没有 epoch/ACK 字段的历史事件重新当作新 prompt 发送。代价是：如果新代码路径产生同形态数据，也会被排除。因此新的 epoch-scoped stream 必须在写出后使用 `MarkCodeSessionInboundEventSentForEpoch` 写入 `delivery_worker_epoch`，不能继续走 legacy mark sent。

---

## 9. 兼容性与边界

- 不要求修改 Claude Code TS 客户端。
- `processed` 可直接从 `sent/received` 跳过 `processing`，因为 TS 里部分路径会直接上报 completed。
- `replBridgeTransport` 当前可能在收到 SSE 后立即上报 `received + processed`，后端按终态幂等处理。
- legacy WebSocket transport 已移除；旧 HTTP poll 路径仍保持现有“写出即 sent”行为，可靠 delivery/replay 只约束 CCR v2 SSE worker stream。
- delivery ACK 不作为 lease 续租信号；worker 活性仍以 heartbeat/lease 为准。
- 当前还没有专门的 ignored ACK metric；如需运营可观测性，后续应加安全截断 event id 的日志或指标。

---

## 10. 当前实现映射

| 模块 | 当前实现 |
|---|---|
| Route | `internal/codesessions/ingress.go` 注册 `/worker/events/delivery` |
| Handler | `handleCodeSessionWorkerDelivery` |
| Body parser | `requireWorkerEpochBody` + `decodeCodeSessionWorkerDeliveryPayload` |
| DB ACK | `ApplyCodeSessionWorkerDeliveryUpdates` |
| ACK event lookup | `getCodeSessionInboundDeliveryEventTx` |
| SSE stream | `handleCodeSessionWorkerEventsStream` + `streamCodeSessionWorkerEvents` |
| mark sent | `MarkCodeSessionInboundEventSentForEpoch` |
| replay query | `ListCodeSessionInboundEventsForWorkerStream` |
| migration | `internal/db/migrations/00007_add_code_session_inbound_delivery_ack.sql` |

---

## 11. 测试覆盖

已有相关测试覆盖：

1. body 校验：
   - 缺 `worker_epoch`；
   - `worker_epoch` 为 `0`、负数、小数、非数字字符串；
   - 缺 `updates`、空数组、超过 64；
   - 缺 `event_id`、非法 `status`。

2. ACK 状态：
   - `received -> processing -> processed` 正常推进；
   - `processing` 隐含 `received`；
   - `processed` 隐含 `received + processing`；
   - 重复 ACK 幂等；
   - 低状态晚到不回退；
   - unknown `event_id` 返回 200 且 `ignored = 1`；
   - queued 事件 ACK 被忽略；
   - 旧发送 epoch 的 ACK 被忽略。

3. event_id 对齐：
   - SSE frame 的 `event_id` 等于 payload `uuid`；
   - delivery 可按 `payload_uuid` 匹配；
   - fallback 到内部 `external_id` 可用。

4. epoch：
   - 当前 epoch 的 delivery 成功；
   - 旧 epoch 返回 409；
   - `worker_epoch = 0` 返回 400，DB ownership helper 直接收到 0 时返回 epoch mismatch。

5. 重发：
   - SSE 写出后只变成 `sent`；
   - 新 epoch stream 重放未 `processed` 事件；
   - `processed` 事件不重放；
   - 同一 epoch 普通重连只返回 `sequence_num > from_sequence_num` 的未完成事件；
   - legacy `sent + delivery_worker_epoch is null + no ACK timestamps` 事件不会在 epoch-scoped stream 中重放；
   - invalid replay cursor 返回 `400`，且不会把 worker 标记为 connected。

相关验证命令：

```bash
go test ./internal/db ./internal/codesessions ./tests -run 'TestCodeSessionWorker' -count=1 -v
```

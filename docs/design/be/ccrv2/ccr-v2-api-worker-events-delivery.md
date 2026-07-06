# 接口文档：`POST /v1/code/sessions/{session_id}/worker/events/delivery`

> CCR v2 worker → server 的**事件送达回执（delivery ACK）**端点。本文整合 HTTP 契约与调用时机机制（含 command lifecycle 触发链路）。
>
> - 后端实现与重发策略见 [`ccr-v2-worker-events-delivery-backend-design.md`](ccr-v2-worker-events-delivery-backend-design.md)
> - worker epoch 语义见 [`ccr-v2-epoch-design.md`](ccr-v2-epoch-design.md)
> - 客户端实现：`src/cli/transports/ccrClient.ts`、`src/utils/commandLifecycle.ts`、`src/cli/remoteIO.ts`

---

## 1. 概述

读方向是**单向 SSE 推送**，HTTP/SSE 没有传输层 ACK。服务端把 client_event（用户消息、控制请求等）推给 worker 后，无法靠网络层确认「worker 收到没、处理到哪一步了」。本端点补上这层**应用级 ACK**：

> worker 用 `{event_id, status}` 告知服务端每个 client_event 的进展，服务端据此**确认送达、追踪处理进度、检测 worker 卡死**。

`status` 是 `received → processing → processed` 的状态机，三个状态由**不同的代码路径**触发，汇聚到同一入口 `CCRClient.reportDelivery(eventId, status)`。

---

## 2. 端点信息

| 项 | 值 |
|---|---|
| Method | `POST` |
| URL | `/v1/code/sessions/{session_id}/worker/events/delivery` |
| 方向 | worker → server |
| 调用方 | `CCRClient.reportDelivery()`（`ccrClient.ts:964`） |
| 上传器 | `SerialBatchEventUploader`（batch 64，队列 64，`ccrClient.ts:410-436`） |
| 鉴权 | `Authorization: Bearer <session_access_token>` |

---

## 3. 请求

### 3.1 Headers

通用写端点 headers：`Authorization`、`Content-Type: application/json`、`anthropic-version: 2023-06-01`、`User-Agent`。

### 3.2 Body

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
| `worker_epoch` | integer 或 string integer | 是 | 当前 worker epoch；必须是正 int64；`0` 是未注册 sentinel，不合法；过期 → 409 |
| `updates` | array | 是 | 批量 ACK，长度 `1..64` |
| `updates[].event_id` | string | 是 | 标识被 ACK 的 client_event，正常应等于 payload `uuid` |
| `updates[].status` | enum | 是 | `received` \| `processing` \| `processed` |

> 批量端点：多条回执攒一批发（`maxBatchSize = 64`），不是一个一发。
>
> 服务端实现必须拒绝 `worker_epoch: 0`。Claude Code 内部默认值可能是 0，但正常 worker 写请求必须先经过 `/worker/register` 或 `/bridge` 获取 `1+` 的 epoch。

### 3.3 服务端 body 处理

当前服务端按以下规则处理请求体：

1. body 必须是 JSON object。
2. `worker_epoch` 必须存在，接受 JSON number 或 string integer；拒绝 `0`、负数、小数和非数字字符串。
3. `updates` 必须存在且长度为 `1..64`。
4. 每个 update 必须包含非空 `event_id` 和合法 `status`。
5. `updates[].status` 只接受 `received | processing | processed`；`sent` 不是 worker ACK，返回 400。
6. 匹配不到 `event_id` 时不返回错误，返回 200 并计入 `ignored`，避免客户端无限重试堵塞 ACK 队列。
7. 匹配到事件但事件还未由当前 epoch 的 SSE stream 标记为 `sent` 时，计入 `ignored`。
8. 匹配到事件但 `delivery_worker_epoch` 不等于请求 `worker_epoch` 时，计入 `ignored`。

例如：

```http
POST /v1/code/sessions/868a223e-be20-43c2-a729-6861af6f8d89/worker/events/delivery
```

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

如果该 `event_id` 已经通过 epoch 1 的 `GET /worker/events/stream` 写出并标记为 `sent`，服务端会补齐 `received_at` 与 `processing_at`，状态推进到 `processing`，并返回 `applied = 1`。如果它仍是 `queued`、属于旧发送 epoch，或根本不存在，则返回 `200` 但计入 `ignored`。

---

## 4. 响应

- **客户端不消费响应体**：经 `CCRClient.request()`（`ccrClient.ts:417`），该方法只判 status，不读 body；
- **当前成功响应体**：

```json
{
  "ok": true,
  "applied": 1,
  "ignored": 0
}
```

- **状态码**：遵循通用契约（`ccrClient.ts:582-630`）——

| 状态码 | 语义 |
|---|---|
| `200` | 请求已处理；未知、未发送或旧发送 epoch 的 event 可被忽略并计入 `ignored` |
| `400` | JSON、`worker_epoch`、`updates` 或 `status` 非法 |
| `409` | epoch 不匹配（worker 已被取代） |
| `401` / `403` | 鉴权失败 |
| `404` | code session 不存在 |
| `429` | 限流，响应头 `Retry-After`（整数秒） |
| `5xx` | 服务端错误 |

- **失败处理**：`deliveryUploader` 未设 `maxConsecutiveFailures` → **失败无限重试**（ACK 不能丢）。

---

## 5. status 状态机

```
received ──► processing ──► processed
 (已送达)     (处理中)        (处理完)
```

后端落库按单调状态机处理：

| 收到 status | 服务端含义 |
|---|---|
| `received` | worker 的 SSETransport 已收到事件 |
| `processing` | 隐含 `received`，说明 worker 已开始处理对应命令 |
| `processed` | 隐含 `received + processing`，说明该事件已完成，后续不应重发 |

重复 ACK 幂等；低状态晚到不能把高状态回退。当前实现中，只要 ACK 通过“当前 epoch 已发送”门槛，重复或低状态晚到也会计入 `applied` 并刷新 `last_delivery_update_at`。

| status | 含义 | 触发方 |
|---|---|---|
| `received` | 事件已抵达 worker 进程 | SSE 接收回调 |
| `processing` | 用户命令开始处理 | command lifecycle `started` |
| `processed` | 用户命令处理完成 | command lifecycle `completed` |

---

## 6. 调用时机（三态触发链路）

### 6.1 `received` —— SSE 刚收到一条 client_event

```
SSETransport 收到 event:client_event 帧
  → handleSSEFrame 调 onEventCallback(ev)
  → CCRClient 构造时注册的 setOnEvent:
      this.reportDelivery(event.event_id, 'received')   // ccrClient.ts:443-444
```

- **粒度**：每一条 SSE 推送的 client_event（`user` / `control_request` / `control_response` 等都算）；
- **event_id 来源**：SSE 帧 `StreamClientEvent.event_id`（服务端分配）。

### 6.2 `processing` / `processed` —— 命令生命周期

经 command lifecycle 机制（`utils/commandLifecycle.ts`，一个全局 listener）中转：

```
notifyCommandLifecycle(uuid, 'started' | 'completed')     // 全局 listener
  → remoteIO.ts:159 注册的 setCommandLifecycleListener:
      const LIFECYCLE_TO_DELIVERY = { started:'processing', completed:'processed' }
      this.ccrClient.reportDelivery(uuid, LIFECYCLE_TO_DELIVERY[state])
```

| status | lifecycle | 触发源（`notifyCommandLifecycle` 调用点） | 含义 |
|---|---|---|---|
| `processing` | `started` | `query.ts:1639`、`print.ts:2059` | 用户命令被消费、**开始处理** |
| `processed` | `completed` | `query.ts:236`、`structuredIO.ts:385`（control_response）、`print.ts:2301/2879/4144` | 命令/对话轮**处理完成** |

- **粒度**：用户命令（prompt / task-notification），比 `received` 粗；
- **event_id 来源**：command 的 `uuid`。

### 6.3 时序示例

```
SSE 推送 user_message(event_id=E)
  → received(E)                          ─┐
命令开始处理 (lifecycle started)           │  同一事件 E，
  → processing(E)                         │  按 event_id 关联三态
命令处理完成 (lifecycle completed)         │
  → processed(E)                         ─┘
```

如果服务端没有收到 `received(E)`，但后续收到了 `processing(E)` 或 `processed(E)`，当前实现会反推该事件已经被 worker 应用层收到并补齐前序时间戳。

---

## 7. event_id 关联语义

三态用 `event_id` 关联同一事件，但**取值来源不同**：

| status | event_id 来源 |
|---|---|
| `received` | SSE 帧 `StreamClientEvent.event_id`（服务端分配） |
| `processing` / `processed` | command `uuid`（用户消息 uuid） |

> **机制前提**：服务端推送 client_event 时须保证 `event_id` = 该消息的 `uuid`，否则 `received(E)` 与 `processing(E)`/`processed(E)` 无法对上。此点客户端代码不能单独证实，需服务端保证。
>
> 后端实现上，`writeCodeSessionWorkerSSEEvent()` 的 `StreamClientEvent.event_id` 当前优先使用 `payload_uuid`，仅在历史/异常事件没有 `payload_uuid` 时 fallback 到内部 `external_id`。不要把 `csev_...` 这类内部事件 ID 作为常规 SSE `event_id` 发给 Claude Code。

**粒度差异**：`received` 对**每条** client_event 上报（含 control_request/response 等），而 `processing`/`processed` 只对**用户命令**上报 —— 因此并非每条 `received` 都有对应的 `processing`/`processed`。

---

## 8. 批量与可靠性

| 机制 | 值 / 行为 | 出处 |
|---|---|---|
| 批量大小 | 64 / 批 | `ccrClient.ts:414` |
| 队列上限 | 64（满则 `enqueue()` 背压阻塞） | `ccrClient.ts:415` |
| 串行 | 同时最多 1 个 POST 在飞 | `SerialBatchEventUploader.drain` |
| 重试 | **无限重试**（未设 `maxConsecutiveFailures`） | `SerialBatchEventUploader.ts:171` |
| 退避 | 指数 500ms→30s + jitter；429 用 `Retry-After` 并 clamp | `SerialBatchEventUploader.ts:235` |
| 调用方式 | fire-and-forget（`void enqueue`，不阻塞主流程） | `ccrClient.ts:968` |

---

## 9. 服务端用途

服务端收到 delivery 回执后当前用于推进 inbound event 的 delivery 状态，并可为后续观测提供依据：

1. **确认送达** —— 没有 `received` 的事件只能说明服务端写出过 SSE，但 worker 未应用层确认；
2. **追踪进度** —— 前端 UI 据 `processing`/`processed` 显示「worker 正在处理 / 已完成」；
3. **卡死检测依据** —— 长期停在 `sent`/`received`/`processing` 未推进的事件，可以作为后续 log/metric 或重新调度判断依据。

如果服务端写出 SSE 后没有收到 `received`：

- 事件保持 `delivery_status = sent`，不能视为完成；
- 不应在同一条健康 SSE 连接内立即重复推送，避免制造重复 prompt；
- 如果后续收到 `processing` 或 `processed`，按隐含状态补齐 `received`；
- 当前实现没有专门的 ACK lag metric 或自动切 epoch 逻辑；
- worker 活性仍由 heartbeat/lease 判断；新 worker register/bridge bump epoch 后，再按 stream 重发规则处理未完成事件。

---

## 10. 服务端重发语义

重发不通过 delivery 端点。delivery 端点只接收 worker ACK。

服务端重发发生在 worker 重新连接：

```http
GET /v1/code/sessions/{session_id}/worker/events/stream
```

当前策略：

1. SSE 写出成功后只标记 `sent`，不能标记 `received` 或 `processed`。
2. 重发时必须复用同一个稳定 `event_id`，即 payload `uuid`。
3. 带 `worker_epoch` 的 stream 按 `from_sequence_num` query 或 `Last-Event-ID` header 得到 `afterSequence`，缺省为 `0`；同一条健康连接内继续维护 `lastSentSequence`，避免 polling loop 重复发送同一事件。
4. 当前 epoch stream 读取 `sequence_num > afterSequence` 且未 `processed` 的事件，并要求 `code_sessions.current_worker_epoch = worker_epoch`。
5. 新 epoch 接管时，如果不带 replay cursor，则从 `afterSequence=0` 开始读取未 `processed` 的事件，包括 `queued`、`sent`、`received`、`processing`。
6. `processed` 是终态，不应重发。
7. 当前实现没有单独的 delivery cutover marker；epoch-scoped stream 会排除 legacy `sent + delivery_worker_epoch is null + no ACK timestamps` 形态，避免升级后重放旧实现已写出的历史事件。
8. 带 epoch 的 stream 会在解析 `from_sequence_num` / `Last-Event-ID` 成功后才标记 connected；非法 cursor 返回 400，不会污染 worker connection 状态。

更完整的 DB 字段、查询和实现细节见 [`ccr-v2-worker-events-delivery-backend-design.md`](ccr-v2-worker-events-delivery-backend-design.md)。

---

## 11. 相关端点

| 端点 | 关系 |
|---|---|
| `GET /worker/events/stream` | delivery 的 `received` 由它的 `client_event` 帧触发；服务端重发也发生在这里 |
| `POST /worker/events` | worker 输出事件（与 delivery 方向相反，都是 worker→server） |
| `POST /worker/heartbeat` | 另一种存活信号（周期性），delivery 是事件级 ACK |

---

## 12. 参考资料

| 文件 | 内容 |
|---|---|
| `internal/codesessions/ingress.go` | 当前服务端 route、body 解析、delivery handler、SSE stream |
| `internal/db/code_sessions.go` | ACK 落库、mark sent、stream replay 查询 |
| `internal/db/migrations/00007_add_code_session_inbound_delivery_ack.sql` | delivery ACK 字段与索引 |
| `cli/transports/ccrClient.ts:410-436` | `deliveryUploader` 配置（batch 64、send 实现） |
| `cli/transports/ccrClient.ts:443-444` | `received` 触发（SSE `setOnEvent` 回调） |
| `cli/transports/ccrClient.ts:964-969` | `reportDelivery` 入口 |
| `cli/remoteIO.ts:155-161` | `setCommandLifecycleListener` + `LIFECYCLE_TO_DELIVERY` 映射 |
| `utils/commandLifecycle.ts` | command lifecycle 全局 listener 机制 |
| `query.ts:236,1639` / `structuredIO.ts:385` / `print.ts:2059,2301,2879,4144` | `started`/`completed` 触发源 |
| `cli/transports/SerialBatchEventUploader.ts` | 批量串行上传器（重试 / 背压） |

# POST /v1/code/sessions/{id}/worker/heartbeat 服务端实现说明

## 概述

`POST /v1/code/sessions/{id}/worker/heartbeat` 已实现为“当前 worker epoch 的租约续期”接口。强一致状态来源是 PostgreSQL 的 `code_sessions` 行；本接口不引入 Redis、不新增 `session_heartbeats` 表，也不通过后台清理任务驱动活性判断。

核心目标：

- 只允许当前 `current_worker_epoch` 的 worker 续约。
- 使用固定 `60s` TTL 和固定 `10s` grace 处理边界延迟。
- 在 epoch 冲突或租约超过 grace 后拒绝心跳，并且不更新任何 session 状态。
- 使用 OMA 签发的 Ed25519 session-ingress JWT，并将签名 `session_id` 与请求路径绑定；session 与 lease 状态由 heartbeat 数据库状态机判断。

相关实现文件：

- `internal/codesessions/ingress.go`
- `internal/db/code_sessions.go`
- `internal/db/db.go`
- `tests/sessions_api_test.go`

## API 行为

### 请求

```http
POST /v1/code/sessions/{code_session_id}/worker/heartbeat
Authorization: Bearer sk-ant-si-<JWT>
Content-Type: application/json
```

请求体必须是 JSON object：

```json
{
  "session_id": "cse_...",
  "worker_epoch": 1
}
```

字段规则：

- `session_id` 必须存在，必须是非空字符串，并且必须等于 path 中的 `{code_session_id}`。
- `worker_epoch` 必须存在，接受正整数 JSON number 或数字字符串。
- `worker_epoch` 拒绝 `0`、负数、浮点、空字符串、非数字、`null` 和 `int64` 溢出值。
- 空 body、malformed JSON、数组 body 都返回 `400 invalid_request_error`。

### 成功响应

```json
{
  "ok": true,
  "worker_lease_expires_at": "2026-07-01T02:00:02.008944Z"
}
```

`worker_lease_expires_at` 使用 `time.RFC3339Nano` 格式。当前 Superduck 客户端只依赖 2xx 成功状态；响应体用于调试和后续兼容，不应作为客户端唯一活性来源。

### 错误映射

| 场景 | HTTP | error.type | 状态更新 |
| --- | --- | --- | --- |
| body 非法或字段非法 | `400` | `invalid_request_error` | 无 |
| ingress token 无效 | `401` | `authentication_error` | 无 |
| code session 不存在 | `404` | `not_found_error` | 无 |
| worker 未注册 | `404` | `not_found_error` | 无 |
| `worker_epoch` 不等于当前 epoch | `409` | `conflict_error` | 无 |
| 当前 epoch 租约超过 grace | `410` | `session_expired` | 无 |
| DB 或系统异常 | `500` | `api_error` | 无 |

## 鉴权

当前实现只接受 OMA 签发的 session-ingress JWT：

```go
Authorization: Bearer sk-ant-si-<JWT>
```

`authorizeSessionIngress` 固定校验 `EdDSA`、`kid`、issuer、audience，并将 JWT `session_id` 绑定到 path 中的 code session。当前不回查数据库 session 状态或 lease；实际是否允许续租由 heartbeat 的 epoch/lease 状态机决定。新 JWT 不设置独立 `exp`。原始 `cse_...` 不再作为普通 ingress token fallback。

handler 的调用顺序为：

1. 先完成 ingress 鉴权。
2. 再解析 heartbeat body。
3. 最后进入 DB 租约续期逻辑。

## 数据模型

心跳状态直接使用 `code_sessions` 现有列：

- `current_worker_epoch`
- `worker_lease_expires_at`
- `worker_last_heartbeat_at`
- `last_worker_activity_at`
- `connection_status`
- `updated_at`

不新增 migration。worker 是否已注册由以下条件判断：

- `current_worker_epoch > 0`
- `worker_lease_expires_at is not null`

`connection_status` 在 heartbeat 成功时会被写为 `connected`，但读侧后续应优先从 `worker_lease_expires_at` 推导活性，避免仅依赖持久化状态判断 worker 是否仍在线。

## Handler 流程

`handleCodeSessionWorkerHeartbeat` 的处理顺序：

1. 从 chi path 读取 `code_session_id`。
2. 调用 `authorizeSessionIngress` 校验 worker ingress token 的签名与 path 绑定。
3. 调用 `decodeCodeSessionWorkerHeartbeatBody` 严格解析 body。
4. 调用 `RecordCodeSessionWorkerHeartbeat(ctx, codeSessionID, epoch, 60s, 10s)`。
5. 根据 DB sentinel error 映射 HTTP 响应。
6. 成功时返回 `ok` 和新的 `worker_lease_expires_at`。

body 解析与 `PUT /worker`、events、diagnostics 等路径分开，避免复用旧的 `ValidateCodeSessionWorkerEpoch` 路径把“未注册 worker”“租约过期”和“epoch mismatch”混在一起。

## DB 续约流程

`RecordCodeSessionWorkerHeartbeat` 是事务型方法，关键逻辑：

```sql
select id, current_worker_epoch, worker_lease_expires_at
from code_sessions
where external_id = $1 and deleted_at is null
for update
```

事务内判断顺序：

1. `epoch <= 0`：返回 `ErrWorkerEpochMismatch`。
2. session 行不存在：返回 `ErrNotFound`。
3. `current_worker_epoch <= 0` 或 `worker_lease_expires_at is null`：返回 `ErrWorkerNotRegistered`。
4. provided epoch 不等于当前 epoch：返回 `ErrWorkerEpochMismatch`。
5. `now > worker_lease_expires_at + 10s`：返回 `ErrWorkerLeaseExpired`。
6. 其余情况更新租约。

成功更新：

```sql
update code_sessions
set worker_last_heartbeat_at = $1,
    worker_lease_expires_at = $2,
    last_worker_activity_at = $1,
    connection_status = 'connected',
    updated_at = $1
where id = $3
returning worker_lease_expires_at
```

其中：

- `$1 = now`
- `$2 = now + 60s`
- `$3 = locked code_sessions.id`

## Epoch 和租约语义

### 当前 epoch 未过期

当前 epoch heartbeat 成功，租约延长到 `now + 60s`。

### 当前 epoch 刚过期但仍在 grace 内

如果 `now <= worker_lease_expires_at + 10s`，仍接受 heartbeat，并把租约续到 `now + 60s`。

### 当前 epoch 超过 grace

如果 `now > worker_lease_expires_at + 10s`，返回 `410 session_expired`，不更新：

- `worker_last_heartbeat_at`
- `worker_lease_expires_at`
- `last_worker_activity_at`
- `connection_status`
- `updated_at`
- `current_worker_epoch`

过期 heartbeat 不会 bump epoch。下一次 worker 通过 register 注册时，仍由注册流程递增到下一个 epoch。

### 旧 epoch 或未知未来 epoch

只要 provided epoch 不等于 `current_worker_epoch`，统一返回 `409 conflict_error`，并且不更新任何状态。

这覆盖两类场景：

- 旧 worker 在新 worker 注册后继续发送 heartbeat。
- 客户端提交了尚未注册的未来 epoch。

## 并发和一致性

heartbeat 使用 `SELECT ... FOR UPDATE` 锁定目标 `code_sessions` 行，因此与 register 的 epoch bump、其他 heartbeat 更新在同一行上串行化。

该设计保证：

- register 先提交时，旧 epoch heartbeat 看到新 epoch 并返回 `409`。
- heartbeat 先提交时，只会续当前 epoch 的租约；后续 register 仍会 bump 到下一 epoch。
- 多个当前 epoch heartbeat 并发时按行锁顺序依次续约，最终租约以最后提交的 heartbeat 为准。

## 日志

epoch mismatch 和 lease expired 会记录结构化文本日志，字段包括：

- `request_id`
- `code_session_id`
- `provided_epoch`
- `current_epoch`
- `worker_lease_expires_at`
- `reason`

日志不得记录 bearer token、worker token 或完整请求 body。

## 客户端兼容性

按当前 Superduck 客户端行为：

- `2xx`：视为 heartbeat 成功。
- `409`：客户端会退出当前 worker 流程，避免旧 worker 继续写入。
- `410`、`5xx`、网络错误：客户端记录失败，并等待下一次 heartbeat。

服务端的 `410` 表示该 epoch 的租约已经超过 grace，不会靠后续同 epoch heartbeat 恢复。真正恢复应通过 register 获取新 epoch。

## 测试覆盖

主要覆盖在 `tests/sessions_api_test.go`：

- invalid auth 返回 `401 authentication_error`。
- malformed JSON、非 object body、缺失或不匹配 `session_id` 返回 `400 invalid_request_error`。
- 缺失或非法 `worker_epoch` 返回 `400 invalid_request_error`。
- 未注册 worker 返回 `404 not_found_error`。
- session 不存在返回 `404 not_found_error`。
- 旧 epoch 和未知未来 epoch 返回 `409 conflict_error`，并断言租约和 activity 字段不变。
- 当前 epoch 未过期 heartbeat 返回 `200` 并续到 `now + 60s`。
- 刚过期但在 `10s` grace 内 heartbeat 返回 `200`。
- 超过 grace 返回 `410 session_expired`，并断言状态不更新。
- 过期 heartbeat 不 bump epoch，下一次 register 继续递增到下一 epoch。
- `worker_epoch` JSON number 和数字字符串都可用。
- 成功响应体包含 `ok` 和 RFC3339Nano `worker_lease_expires_at`。

验证命令：

```bash
go test ./tests -run 'TestCodeSessionWorker.*Heartbeat|TestCodeSessionWorkerEpochProtection|TestCodeSessionWorkerEpochZeroRejectedAtDBLayer|TestCodeSessionWorkerEpochValidationRejectsInvalidValues' -count=1
go test ./internal/db ./internal/codesessions -count=1
```

仓库级验证命令仍是：

```bash
go test ./... -count=1
```

## 本次非目标

以下能力不属于当前实现：

- Redis TTL 存储。
- `session_heartbeats` 新表。
- 全局、用户级或 session 级 heartbeat rate limit。
- nonce 防重放。
- 真实 JWT 签名验证。
- 独立后台任务清理 heartbeat 状态。
- 跨区域一致性协议。

这些能力需要独立认证、限流或部署设计。当前接口的强一致边界是单行 PostgreSQL 事务。

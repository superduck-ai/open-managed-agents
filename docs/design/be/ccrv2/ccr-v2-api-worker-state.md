# 接口文档：`GET/PUT /v1/code/sessions/{session_id}/worker`

本文记录当前后端实现中的 code-session worker state API。该接口用于持久化 worker 的轻量状态和 metadata patch，不负责 worker event 生成、delivery ACK、webhook job 或 session event 合成。

相关代码：

- HTTP handler: `internal/codesessions/ingress.go`
- DB helper: `internal/db/code_sessions.go`
- migrations: `internal/db/migrations/00003_add_code_session_worker_epoch.sql`、`00004_add_code_session_worker_state.sql`、`00005_ensure_code_session_worker_state.sql`、`00006_ensure_code_session_worker_epoch_default.sql`

## 1. Scope

`/worker` 当前实现覆盖三个行为：

| Endpoint | 作用 |
|---|---|
| `POST /v1/code/sessions/{session_id}/worker/register` | 注册新 worker，递增 `current_worker_epoch`，旧 worker 后续写请求变成 stale |
| `PUT /v1/code/sessions/{session_id}/worker` | 按当前 epoch patch worker state / metadata |
| `GET /v1/code/sessions/{session_id}/worker` | 读取当前持久化 worker state；可选校验 epoch |

所有入口使用统一 ingress auth：Bearer token 必须是 OMA 签发的 `sk-ant-si-<JWT>`，并通过签名、固定 claims 和 session path 绑定校验；当前不在 JWT 鉴权阶段回查数据库 session 状态或 worker lease。

## 2. 数据模型

`code_sessions` 当前相关字段：

```sql
current_worker_epoch bigint not null default 0
worker_lease_expires_at timestamptz
worker_registered_at timestamptz
worker_last_heartbeat_at timestamptz
worker_token_session_id text
worker_binding jsonb not null default '{}'::jsonb

worker_status text not null default 'idle'
worker_external_metadata jsonb not null default '{}'::jsonb
worker_requires_action_details jsonb
```

`worker_status` 由 check constraint 限制为：

```text
idle | running | requires_action
```

migration 状态：

- `00003_add_code_session_worker_epoch.sql` 添加 epoch、lease、binding 字段，`current_worker_epoch` 初始为 `0`。
- `00004_add_code_session_worker_state.sql` 添加 worker state 字段，backfill `worker_status='idle'`、`worker_external_metadata={}`，并设置 not-null/default/check。
- `00005_ensure_code_session_worker_state.sql` 当前是 no-op，占位给 prerelease 分支中过去曾出现过的版本 5。
- `00006_ensure_code_session_worker_epoch_default.sql` 将 `current_worker_epoch` default 固定为 `0`，把历史 `null` 或负数修正为 `0`，并 enforce not-null。

## 3. Register

`POST /v1/code/sessions/{session_id}/worker/register`

请求 body 可为空。若提供 JSON body，只识别可选 `session_id`：

```json
{
  "session_id": "cse_..."
}
```

规则：

- body 为空：允许。
- body 非法 JSON：`400 invalid_request_error`。
- `session_id` 存在且不等于 path 中的 code session id：`400 invalid_request_error`。

DB 行为：

1. `select code_sessions ... for update` 锁住当前 code session 行。
2. `next_epoch = current_worker_epoch + 1`。
3. 更新 lease/binding/connected/activity 字段。
4. 返回新 epoch。

响应：

```json
{
  "worker_epoch": "1"
}
```

`worker_epoch` 以 JSON string 返回。第一次 register 返回 `"1"`；每次合法 register 都会递增 epoch，不复用旧 epoch。

## 4. PUT Worker State

`PUT /v1/code/sessions/{session_id}/worker`

### Request

请求必须是 JSON object，`worker_epoch` 必填：

```json
{
  "worker_epoch": 1,
  "worker_status": "requires_action",
  "requires_action_details": {
    "tool_name": "Bash",
    "action_description": "Running npm test",
    "request_id": "req_..."
  },
  "external_metadata": {
    "pending_action": { "tool_name": "Bash" },
    "task_summary": null
  }
}
```

字段规则：

| 字段 | 类型 | 必填 | 当前实现 |
|---|---|---:|---|
| `worker_epoch` | JSON number 或 string integer | 是 | 必须是正 int64；`0`、负数、小数、非数字字符串、`null` 都是 400 |
| `worker_status` | string | 否 | trim 后必须是 `idle`、`running`、`requires_action` |
| `requires_action_details` | object 或 `null` | 否 | object 会先作为候选 details；最终 status 不是 `requires_action` 时会被清空 |
| `external_metadata` | object | 否 | 按一层 merge patch 应用；value 为 JSON `null` 时删除该 key |

未知字段当前会被忽略。

### Epoch

`PUT` 在 DB 事务内执行：

1. `select code_sessions ... for update`。
2. 若 request `worker_epoch != current_worker_epoch`，返回 `409 conflict_error`。
3. 匹配时才应用 state patch。

`PUT` 不会 bump epoch；只有 `/worker/register` 会注册新 worker 并递增 epoch。

### Patch 语义

`worker_status` 未提供时保留当前 `worker_status`。

`requires_action_details` 的最终不变量：

```text
final worker_status == requires_action  => 可保存 object 或 null
final worker_status != requires_action  => 一律保存为 null
```

因此以下请求在当前 status 为 `running` 时会成功，但不会保存 details，也不会把 public session 改成 idle：

```json
{
  "worker_epoch": 1,
  "requires_action_details": { "tool_name": "Bash" }
}
```

`external_metadata` 是一层 merge，不做 deep merge：

```json
{
  "external_metadata": {
    "pending_action": null,
    "task_summary": "done",
    "nested": { "a": 1 }
  }
}
```

- `pending_action` 从现有 metadata 删除。
- `task_summary` 设置或覆盖为 `"done"`。
- `nested` 整个 key 被替换为 `{ "a": 1 }`。
- 如果 merge 后为空对象，DB 存 `{}`。

### Side effects

成功 PUT 会更新：

- `worker_status`
- `worker_requires_action_details`
- `worker_external_metadata`
- `connection_status='connected'`
- `last_worker_connected_at=now`
- `last_worker_activity_at=now`
- `updated_at=now`

如果请求中显式提供了 `worker_status`，handler 会同步 public session 和 primary thread 状态：

| `worker_status` | public `sessions.status` / primary thread status |
|---|---|
| `running` | `running` |
| `idle` | `idle` |
| `requires_action` | `idle` |

同步发生在 worker state DB 更新提交之后。同步时忽略 `ErrNotFound`；其它 DB 错误会让请求返回 `500 api_error`，此时 worker state 已经持久化，但 handler 不返回成功响应，避免调用方误以为 public status 也同步完成。`requires_action` 不是 public session status enum；阻塞语义由 worker state 和 metadata 表达。

如果请求没有显式 `worker_status`，不会触发 public session/thread 状态同步。details-only update 会保留当前 public status。

### Response

成功返回当前持久化 state：

```json
{
  "ok": true,
  "session_id": "cse_...",
  "status": "connected",
  "worker_epoch": "1",
  "connection_url": "https://example.com/v1/code/sessions/cse_.../worker",
  "worker": {
    "external_metadata": {
      "pending_action": { "tool_name": "Bash" }
    },
    "internal_metadata": null,
    "worker_epoch": "1",
    "worker_status": "requires_action",
    "requires_action_details": {
      "tool_name": "Bash",
      "action_description": "Running npm test",
      "request_id": "req_..."
    }
  }
}
```

注意：

- 顶层 `status` 是 worker connection status，例如 `connected`，不是 `worker_status`。
- `worker.worker_epoch` 和顶层 `worker_epoch` 都是 string。
- `external_metadata` 为空时返回 `{}`。
- `requires_action_details` 为空时返回 JSON `null`，不是省略字段。
- `internal_metadata` 当前固定返回 `null`。

## 5. GET Worker State

`GET /v1/code/sessions/{session_id}/worker`

无 body。该接口用于 worker 重启后恢复前一个 worker 写入的外部 metadata。当前客户端只消费
`worker.external_metadata`，不会读取 PUT response 中的调试字段或 worker status 字段。

```http
GET /v1/code/sessions/cse_.../worker
```

可选传 `worker_epoch` 做 ownership 校验：

```http
GET /v1/code/sessions/cse_.../worker?worker_epoch=1
```

也支持 header：

- `x-worker-epoch`
- `worker-epoch`
- `worker_epoch`

规则：

- 不传 epoch：只读取当前 metadata，不刷新 `connection_status` 或 activity 时间。
- 传 epoch：必须是正整数；不合法返回 `400 invalid_request_error`。
- 传 epoch 且不等于当前 epoch：返回 `409 conflict_error`。
- 传 epoch 且匹配：返回当前 metadata；仍然不刷新 connected/activity 字段。

成功响应是 GET 专用的最小 shape：

```json
{
  "worker": {
    "external_metadata": {
      "pending_action": { "tool_name": "Bash" },
      "task_summary": "Running tests"
    }
  }
}
```

当 metadata 为空对象、`null` 或空值时，省略 `external_metadata`，返回：

```json
{
  "worker": {}
}
```

GET 不返回 `ok`、`session_id`、`status`、`worker_epoch`、`worker_status`、`requires_action_details`、`connection_url` 等 PUT response 字段。

## 6. Error Shape

所有错误通过 `internal/httpapi.WriteError` 返回 Anthropic-compatible error shape。

| 场景 | HTTP |
|---|---|
| ingress auth 失败 | `401 authentication_error` |
| code session 不存在 | `404 not_found_error` |
| body 超过 `maxIngressBodySize` | `413 invalid_request_error` |
| body 非 JSON object、缺 `worker_epoch`、字段类型非法 | `400 invalid_request_error` |
| `worker_epoch` 过期或不匹配 | `409 conflict_error`，message 为 `Worker epoch mismatch` |
| DB 更新或 public status 同步异常 | `500 api_error` |

## 7. 与其它 Worker Endpoint 的边界

`PUT /worker` 只持久化 worker state，不合成 session events，不写 webhook jobs，也不处理 outbound delivery ACK。

相关但独立的接口：

| Endpoint | 说明 |
|---|---|
| `POST /worker/events` | worker 输出事件；要求 epoch，并在 event append 事务内检查 epoch |
| `POST /worker/internal-events` | internal worker event；要求 epoch |
| `POST /worker/events/delivery` | worker 对 SSE `client_event` 的 ACK |
| `POST /worker/heartbeat` | 续当前 epoch lease |
| `GET /worker/events/stream` | worker 读取 queued inbound events；epoch 可选但带 epoch 时会做 ownership 保护 |

## 8. 当前测试覆盖

主要覆盖在 `tests/sessions_api_test.go`：

- register 第一次返回 `1`，再次 register 递增 epoch；旧 epoch PUT 返回 409。
- PUT 初始化 `idle`，删除 stale `pending_action` / `task_summary` metadata。
- `external_metadata` 一层 merge 保留旧 key，JSON `null` 删除 key。
- `requires_action` 保存 details 和 `external_metadata.pending_action`。
- `running` / `idle` 清空 `requires_action_details`。
- 当前 status 为 `running` 时，details-only PUT 不保存 details，且 public session/thread 保持 `running`。
- `worker_status=running` 同步 public session/thread 为 `running`。
- `worker_status=idle` 或 `requires_action` 同步 public session/thread 为 `idle`。
- GET `/worker` 用最小 response 读回 PUT 后的 non-empty `external_metadata`。
- GET `/worker` metadata 为空时返回 `{ "worker": {} }`，且不刷新 connected/activity。
- 缺失或非法 `worker_epoch`、非法 `worker_status`、非 object metadata/details 返回 400。

推荐验证命令：

```bash
go test ./tests -run TestCodeSessionWorker -count=1
go test ./internal/codesessions ./internal/db -count=1
```

# 接口文档：`GET/PUT /v1/code/sessions/{session_id}/worker`

本文记录当前后端实现中的 code-session worker state API。该接口用于持久化 worker 的轻量状态和 metadata patch，不接收 worker 输出事件或 delivery ACK。显式运行状态会驱动公开 Session/Thread 状态；公开事件及状态转换由事件写入事务统一提交。

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
| `worker_epoch` | JSON number | 是 | 必须是正 int64；`0`、负数、小数、字符串、`null` 都是 400 |
| `worker_status` | string 或 `null` | 否 | 非空时必须精确为 `idle`、`running`、`requires_action`；缺省、空字符串或 `null` 不更新状态 |
| `requires_action_details` | object 或 `null` | 否 | 按客户端 schema 解析 `tool_name`、`action_description`、`tool_use_id`、`request_id`、`input`；未知字段忽略；最终 status 不是 `requires_action` 时会被清空 |
| `external_metadata` | object 或 `null` | 否 | object 按一层 merge patch 应用；顶层 `null` 不更新 metadata；object 内的 value 为 JSON `null` 时删除该 key |

未知字段当前会被忽略。

### Epoch

`PUT` 在 DB 事务内执行：

1. `select code_sessions ... for update`。
2. 若 request `worker_epoch != current_worker_epoch`，返回 `409 conflict_error`。
3. 匹配时才应用 state patch。

`PUT` 不会 bump epoch；只有 `/worker/register` 会注册新 worker 并递增 epoch。

### Patch 语义

`worker_status` 未提供、为空字符串或为 `null` 时保留当前 `worker_status`。`requires_action_details` 和 `external_metadata` 未提供或为顶层 `null` 时也按零值处理，不单独更新对应字段。worker status 最终不是 `requires_action` 时，DB 不变量仍会清空已有 details。

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
| `idle` | 仅当前 Worker 已显式上报 running 时同步为 `idle`；初始化 idle 不结束已接受的任务 |
| `requires_action` | `idle` |

同步发生在 worker state DB 更新提交之后。`running`、`idle` 和 `requires_action` 都通过同一条 public event 管道同步：先持久化 `session.status_running` 或 `session.status_idle`，再由 session event projection 更新 public session 与 primary thread，随后广播 SSE，并且只为首次创建的事件投递 webhook。同一 public 状态下重复上报不会生成重复事件；状态发生转换后再次上报会生成新的事件。

`requires_action` 不是 public session status enum，映射为带 `requires_action` 原因的 `session.status_idle`。

迁移 `00064_session_input_state.sql` 添加内部 `worker_turn_started` 标记。注册 Worker 和接受新一轮主线程输入时清零，
仅显式 running 上报置为 true；普通 idle 和 metadata-only 更新保留该标记。
因此初始化 idle 不会生成公开结束事件，正常结束发布失败后的重复 idle 仍可重试。
该标记不进入 Worker 或 Session API 响应。

公开事件插入、primary thread 与 Session 状态更新在同一事务提交，失败时一起回滚，PUT 返回 `500 api_error`。Worker 使用相同状态重试时可补齐失败的事务；已存在的事件不会重新推动状态，也不会重复广播 SSE 或投递 webhook，避免迟到的旧事件覆盖新状态。

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
- `worker_status=running` 持久化一个 `session.status_running`，并通过该事件同步 public session/thread 为 `running`。
- 同一 running 状态的重复 PUT 不重复生成事件；经过 idle 后的新一轮 running 会生成新事件。
- 初始化和新一轮开始前的重复 `worker_status=idle` 不覆盖 public running；执行开始后的 idle 或 requires_action 同步 public session/thread 为 idle。
- GET `/worker` 用最小 response 读回 PUT 后的 non-empty `external_metadata`。
- GET `/worker` metadata 为空时返回 `{ "worker": {} }`，且不刷新 connected/activity。
- 缺失或非法 `worker_epoch`、非法 `worker_status`、非 object metadata/details 返回 400。

推荐验证命令：

```bash
go test ./tests -run TestCodeSessionWorker -count=1
go test ./internal/codesessions ./internal/db -count=1
```

## 公共输入处理 ACK

Worker delivery 的 processing 对应用户命令 started，processed 对应 completed；控制响应在应用后上报 processed。
空闲主线程接纳的第一条 `user.message` 在发送事务中就设置 `processed_at = created_at` 并广播；已有排队消息或等待工具确认时仍排队，Session/Thread 状态和 `worker_turn_started` 不变。接纳事务按 Session → Code Session 加锁读取 Worker 状态和 metadata，再统一决定输入时间、running 事件及状态更新；拒绝接纳时只写排队输入。SQL 不解析待确认规则，接纳和公开 stop_reason 都使用 `managedagentsevents.PendingToolEventIDs`，按有效请求、metadata 键及默认主线程的相同规则判断。同为 idle 时，等待原因或工具 ID 集合改变仍产生新的状态事件。公开输入的 ID 保留在投递 envelope 中，ACK 只将排队事件的 processed_at 从 null 推进到处理时间，并只广播一次。已接纳消息的 ACK 不修改时间，也不重复广播。
写入时按 Session → Code Session 的顺序加锁并校验 epoch，避免旧 Worker 修改新 epoch 的输入状态。
该步骤先于 broker ACK 行锁执行，防止与输入接受事务的锁顺序倒置。
控制响应的顶层 `id` 保留原始公开输入 ID，ACK 因此能更新对应工具确认；控制响应自己的 `uuid` 保持不变。`system.message` 不投递给 Worker，接收时直接处理。
Worker 早到的 running 不覆盖仍待确认的主线程；待确认 metadata 仍沿用控制响应入队成功后清理的现有路径。

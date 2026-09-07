# 接口文档：`GET/PUT /v1/code/sessions/{session_id}/worker`

本文记录当前后端实现中的 code-session worker state API。该接口用于持久化 worker 的轻量状态和 metadata patch；显式上报状态时，同步提交相应公共状态事件。delivery ACK 仍由独立接口处理。

公共事件写入与本状态接口共用 epoch 校验和事务：同 ID 内容冲突返回 409，不提交同批之前的事件或状态。canonical error 字段及 result 结束原因尚未统一。worker 瞬态 idle 仍可能提前产生 `end_turn`，详见 [Session 一致性方案](../../session-event-stream-consistency.md#升级与边界)。

相关代码：

- HTTP handler: `internal/codesessions/ingress.go`
- Service: `internal/codesessions/status.go`
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

1. 按 Session → Code Session 顺序加行锁；已归档的 Session 返回 `409 conflict_error`。
2. 在锁内复验请求原始 `worker_epoch`，不匹配返回 `409 conflict_error`。
3. 在同一个 `ManagedAgentEventTx` 中应用 worker patch、判断公共状态是否变化、写入事件并推进公共状态。任一步失败全部回滚。

worker 字段更新通过同一 executor 的 Yourbatis Mapper 执行，按 workspace/Code Session UUID 定位并排除软删除记录。

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

如果请求中显式提供了 `worker_status`，service 在事务中按 primary thread 当前状态判断是否需要同步：

| `worker_status` | primary thread status（Session 另行聚合） |
|---|---|
| `running` | `running` |
| `idle` | `idle` |
| `requires_action` | `idle` |

worker state、`session.thread_status_running` / `session.thread_status_idle`、必要的聚合 Session 状态事件、Session/primary thread projection 与公共事件时钟一起提交；提交后才通知 SSE 和投递现有 webhook。SSE 从数据库有序补读完整事件，刷新历史读取相同记录。显式状态上报还保留原有的子线程 transcript 物化，物化失败也回滚本次 worker patch。

状态去重在 Session 锁内比较 primary thread；并发的相同上报只产生一条公共状态事件。主线程首次 idle 或重复 idle 时，只要子线程仍 running，聚合 Session 就保持 running，不发送 Session idle；首次转换仍保存线程 idle 事实。所有线程状态优先级为 running > rescheduling > idle > terminated。状态实际转换后会生成新事件。该去重不代表已完成所有生命周期入口的统一仲裁。

`requires_action` 映射为 primary 的 `session.thread_status_idle`。聚合 Session idle 时，从 source Code Session 的待确认权限记录与本次请求合并 `requires_action.event_ids`；有子线程运行则不发 Session idle。单独 PUT 的 details 不提供可验证的 public event ID，没有权限记录时仍使用默认 `end_turn`，这部分尚未完整对齐。新线程事件从对应快照补 `agent_name`，重试保留原字段。

事件、projection 或 worker patch 写入失败时，PUT 返回 `500 api_error`，数据库保留本次调用前的记录和处理水位。成功后重复上报同一状态不推进公共事件时钟，也不重放旧状态；私有连接/activity 字段仍按原接口合同更新。请求未携带 turn ID 或状态命令 ID，同一 epoch 内跨状态的迟到请求仍需后续 turn 合同解决。

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
| Session 已归档 | `409 conflict_error` |
| DB 更新或 public status 同步异常 | `500 api_error` |

## 7. 与其它 Worker Endpoint 的边界

`PUT /worker` 将 worker state 与必要的公共状态事件一起提交，并在提交后通知既有事件分发路径；不处理 delivery ACK，也不把 worker 的 idle 上报视为输入已应用或模型请求结束的事实。

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
- `worker_status=running` 持久化线程 running 事实，按需产生聚合 Session running；事件与投影原子提交。
- 同一 running 状态的重复 PUT 不重复生成事件；经过 idle 后的新一轮 running 会生成新事件。
- `worker_status=idle` 或 `requires_action` 将 primary thread 转为 idle；Session 状态根据全部线程聚合，待确认请求不会被其他线程的 end_turn 覆盖。
- GET `/worker` 用最小 response 读回 PUT 后的 non-empty `external_metadata`。
- GET `/worker` metadata 为空时返回 `{ "worker": {} }`，且不刷新 connected/activity。
- 缺失或非法 `worker_epoch`、非法 `worker_status`、非 object metadata/details 返回 400。

新增 `tests/session_event_worker_state_test.go` 覆盖公共写入失败回滚完整私有快照、私有 metadata 写入失败、details 与大整数 metadata patch、重复/并发状态去重，以及子线程 running 时重复主线程 idle。`tests/session_event_consistency_test.go` 在持有 Session 锁的竞态窗口切换 epoch，验证旧 PUT 不能更新私有状态、metadata 或公共事件。

推荐验证命令：

```bash
go test ./tests -run 'TestCodeSessionWorker|TestWorkerStateCommits|TestWorkerStatusSerializes|TestWorkerEpochSwitch' -count=1
go test ./internal/codesessions ./internal/db -count=1
```

# session.usage 事件（累计用量快照 + budget 回显）

> Issue: #249
> 日期: 2026-08-16
> 分支: `feat/session-usage-event`

## 背景

官方 Claude Managed Agents 的事件类型表里有 `session.usage` 事件：会话**累计用量**快照，携带 usage 对象 + budget（无预算时 null）。**触发时机**：idle 转换时（每次 session 进入 idle 前发一个；预算暂停时的额外发送属 #244 后续）——`https://platform.claude.com/docs/en/managed-agents/events-and-streaming.md`。

OMA 当前无此事件：用量只在 `span.model_request_end`（`mapper.go:392-441`）有单次请求的用量，session 级累计存在 DB 快照（`sessions.usage`），从不作为事件发出。

## 方案

### 1. `publicSessionStatusPayloads` 同时产生 `session.usage`

`internal/codesessions/status.go:45-90`：构造 `session.status_idle` 时，**额外产生 `session.usage` 事件**：
- `usage`：session 的 `Usage` 字段（DB 快照的累计用量）
- `budget`：当前固定为 `null`（`db.Session` 尚无 `Budget` 字段；#244 引入后改为读取 `session.Budget`，见「对抗性 review 修正」的合并检查清单）
- 触发条件：eventType 为 `session.status_idle` 时（idle 转换）

顺序保证：`session.usage` 的 `created_at`/`processed_at` 取 `idle 时间 - 1µs`（`publicEventOrderingGap`）。共用同一时间戳时，两个事件的相对顺序会退化为按 ID 排序：

- 发布排序 `sessions.PublishCodeSessionEvents`：`ProcessedAt → CreatedAt → ExternalID`，最终 tie-break 落到 SHA-256 派生的 `sevt_` ID
- 历史读取 `ListSessionEventsPage`：`ORDER BY created_at ASC, uuid ASC`，`uuid` 是 `gen_random_uuid()` 随机值

`session_events.created_at`/`processed_at` 是 PostgreSQL `timestamptz`（微秒精度），因此差值必须 ≥ 1µs，纳秒级差值会被截断成同一时刻。取 1µs 后顺序在实时 SSE 与落库历史中都严格成立。

排序相关的纯逻辑抽为 `statusTransitionPayloads(record, session, eventType, status, now)`，`publicSessionStatusPayloads` 只负责查库与去重判断，单测可直接断言最终事件序。

### 2. `session.usage` 事件类型注册

`internal/managedagentsevents/events.go`：`CategoryFor` 注册 `session.usage`（CategorySessionStatus），确保事件可持久化/进历史。

## 明确不做

- 用量**累计逻辑**本身（`span.model_request_end` → session.usage 聚合）依赖 #62 用量采集，本实现用 DB 快照现有值
- `budget_reached` 精确暂停语义（#244 后续）
- list cost 计算

## 测试

- `statusTransitionPayloads`：idle 时产生 `[session.usage, session.status_idle]`，且 usage 时间戳严格早于 idle 且差值 ≥ 1µs（落库后保序）
- `statusTransitionPayloads`：非 idle 状态只产生状态事件，不带 usage
- `sessionUsagePayload`：事件结构（type/usage/budget），无 usage 时回显 `{}`
- `CategoryFor("session.usage")` 返回 CategorySessionStatus

## 验收

- session 进入 idle 时，事件流出现 `session.usage`（携带累计用量 + budget/null）
- `session.usage` 在实时流与历史列表中都紧邻 `session.status_idle` 之前
- 官方 SDK 的 `session.usage` 处理分支可正常工作

## 对抗性 review 修正（2026-08-16）

**跨分支合并检查清单（#249 ↔ #244）**：
- [ ] **必须**：`sessionUsagePayload`（status.go）的 `budget` 从硬编码 `null` 改为读取 `session.Budget`（#244 给 db.Session 加了 Budget 字段）。当前硬编码 null 与「无预算」现状一致，但合并 #244 后不会自动生效——这是跨 PR 契约缺口，**必须在合并时手动修改**（无 CI 能捕获）
- [ ] #244 的 `sessionUpdatedEvent` 已带 budget ✅（无需改）
- [ ] 合并后补集成测试：带 budget 的 session idle 时，`session.usage` 事件回显真实 budget

**事件 ID 去重提示**：`stablePublicEventID(..., session.UpdatedAt)` 种子在同一秒内连续 idle 可能重复（频率极低，不阻塞）。

## Review 修正（2026-08-19）

**usage/idle 顺序在落库后不成立**：初版让 usage 与 idle 共用同一个 `now`，两者 `created_at`/`processed_at` 完全相同，顺序退化为按 `ExternalID`（SHA-256 派生）/ `uuid`（随机）排序，实测随 `UpdatedAt` 翻转。修正为 usage 取 `idle - 1µs`（见「顺序保证」），并把排序逻辑抽成 `statusTransitionPayloads` 纯函数，单测直接断言生产代码产出的事件序与时间差。

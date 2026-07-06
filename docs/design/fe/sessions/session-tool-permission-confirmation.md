# Session Tool 权限确认只读回放设计指南


---

## 1. 背景

权限策略有两层含义：

- **配置层**：Agent 的 `tools` 配置决定预构建 agent toolset 与 MCP toolset 是自动执行还是等待确认。详见 [权限策略](../permission-policies.md)。
- **回放层**：Session Detail 页面读取已经写入事件流的 tool use、tool confirmation 和 tool result，还原本次工具调用处于等待、运行、完成、失败或拒绝状态。

本文只设计回放层。Session Detail 不承担审批决策，不渲染 Allow/Deny 按钮，也不主动发送 `user.tool_confirmation`。审批动作发生在 agent 运行客户端或调用方应用中，结果以 `user.tool_confirmation` 事件进入 session event stream。

后端事件输出必须先遵守 Anthropic multi-agent 线程契约，再由前端做归一化回放：

- `/v1/sessions/{session_id}/events` 是 primary thread 的浓缩视图，不应返回普通子线程的完整工具调用历史。
- `/v1/sessions/{session_id}/threads/{session_thread_id}/events` 是子线程明细视图，子线程自身的 tool use/result 应在这里返回。
- 子线程 tool use 只有在需要客户端动作时才允许 cross-post 到 primary，例如 `always_ask` 权限确认或 custom tool result；primary copy 必须携带 `session_thread_id`，用于标识来源子线程并路由后续确认/结果。
- 子线程自己的 thread events 可以不重复携带 `session_thread_id`，因为 URL 已经定位到 thread；若返回时补齐该字段，也只能作为响应层便利字段，不能改变事件归属。

参考来源：

- [../permission-policies.md](../permission-policies.md)：公开 API 契约、默认策略、确认事件发送流程。
- [session-detail-lane-timeline-design.md](session-detail-lane-timeline-design.md)：session detail minimap 与 transcript 的现有设计边界。

---

## 2. 事件契约

### 2.1 Tool use

| 事件类型 | 关键字段 | 说明 |
|---|---|---|
| `agent.tool_use` | `id`, `name`, `input`, `evaluated_permission` | 预构建 agent toolset 调用。 |
| `agent.mcp_tool_use` | `id`, `name`, `input`, `evaluated_permission` | MCP toolset 调用。 |
| `agent.custom_tool_use` | `id`, `name`, `input` | 自定义工具调用，权限策略不适用，但 UI 可复用 tool call 展示路径。 |

`evaluated_permission` 是服务端对本次 tool use 的策略评估结果。前端展示层归一化为：

| 原始值 | 归一化值 | 含义 |
|---|---|---|
| `ask`, `always_ask`, `requires_action` | `ask` | 需要确认后才能执行。 |
| `allow`, `always_allow` | `allow` | 自动放行，工具可直接运行。 |
| `deny`, `denied` | `deny` | 策略直接拒绝，工具不会运行。 |

多线程补充：

- coordinator/primary 自己的 tool use 不需要 `session_thread_id`。
- 子线程自身的 tool use 在 thread endpoint 中以该 thread 作为 owner，不要求 payload 内重复 `session_thread_id`。
- 子线程 tool use 若被 cross-post 到 primary，必须带 `session_thread_id`，且该字段只表示“来源子线程”，不是 primary 事件的 owner。
- 没有 `session_thread_id` 的子线程 tool use 不应出现在 primary；若历史数据或异常 payload 已经这样返回，应由后端响应过滤和 API 测试兜住，不在前端隐藏/去重。
- 同一规则也适用于按 `tool_use_id` 关联的 `tool_result` 与 `user.tool_confirmation`：如果 primary 中出现无 `session_thread_id` 的 result/confirmation，但同一 `tool_use_id` 已在子线程 owner copy 中存在，则 primary 响应必须过滤该 orphan projection。

### 2.2 Tool confirmation

`user.tool_confirmation` 表示某个阻塞 tool use 已被确认：

```ts
interface UserToolConfirmationEvent {
  type: 'user.tool_confirmation'
  tool_use_id: string
  result: 'allow' | 'deny'
  deny_message?: string | null
}
```

`tool_use_id` 必须回指对应 tool use 事件的 `id`。`result="allow"` 表示可以继续执行；`result="deny"` 表示拒绝执行，`deny_message` 可用于说明原因。

当确认的是 cross-post 到 primary 的子线程 tool use 时，调用方可以把 tool use 事件上的 `session_thread_id` 原样带回 `user.tool_confirmation.session_thread_id`。即使只传 `tool_use_id`，服务端也应能根据阻塞事件路由到正确线程；但响应 payload 仍应保留 `session_thread_id`，避免 UI 和 SDK 客户端丢失来源信息。

### 2.3 Tool result

Tool result 事件通过 tool use id 关联到 tool use：

| 事件类型 | 关联字段 | 说明 |
|---|---|---|
| `user.tool_result` | `tool_use_id` | 调用方返回的 tool result。 |
| `agent.tool_result` | `tool_use_id` | agent toolset result。 |
| `agent.mcp_tool_result` | `mcp_tool_use_id` | MCP toolset result。 |
| `user.custom_tool_result` | `custom_tool_use_id` | 自定义工具 result。 |

result 事件中的 `is_error=true` 表示执行失败。下载、流式复制、外部工具错误等细节仍保留在原始 payload 中，Transcript 只消费成功/失败语义和可读摘要。

多线程 result 归属遵循对应 tool use：普通子线程 result 应只出现在 thread endpoint；需要 cross-post 的 result/confirmation 必须携带 `session_thread_id`。历史上 Claude Code 可能把子线程 MCP result 作为无归属 `agent.tool_result` 写到 primary，后端响应层应根据 `tool_use_id` 与子线程 tool use owner copy 去除该 projection，避免 Orchestrator 回放出子线程工具。

---

## 3. 归一化模型

Transcript 不直接逐条渲染原始事件。构建 session trace 时，应先扫描事件流并建立两张索引：

```ts
const toolResultsByUseId = new Map<string, SessionEvent>()
const toolConfirmationsByUseId = new Map<string, UserToolConfirmationEvent>()

for (const event of events) {
  if (isToolResultEvent(event)) {
    const id = toolResultUseId(event)
    if (id) toolResultsByUseId.set(id, event)
  }

  if (event.type === 'user.tool_confirmation') {
    toolConfirmationsByUseId.set(event.tool_use_id, event)
  }
}
```

之后遍历事件生成显示 entry：

- `tool_use` 事件生成 `tool_call` entry。
- 对应 result 通过 `toolResultsByUseId.get(toolUse.id)` 挂到 `tool_call.resultEvent`。
- 对应 confirmation 通过 `toolConfirmationsByUseId.get(toolUse.id)` 挂到 `tool_call.confirmationEvent`。
- Transcript 模式跳过独立的 `tool_result` 与 `user.tool_confirmation` 行。
- Debug 模式仍显示所有原始事件，方便排查事件流和关联错误。

建议内部结构：

```ts
type ToolLifecycle =
  | 'awaiting_approval'
  | 'running'
  | 'failed'
  | 'denied'
  | 'completed'

interface ToolCallEntry {
  kind: 'tool_call'
  name: string
  input: unknown
  resultEvent?: SessionEvent
  confirmationEvent?: UserToolConfirmationEvent
  lifecycle: ToolLifecycle
  bracketId?: string
}
```

---

## 4. Lifecycle 状态机

对每个 `tool_call`，按以下顺序计算 lifecycle：

```ts
function toolLifecycle(toolUse, resultEvent, confirmationEvent): ToolLifecycle {
  const permission = normalizedPermission(toolUse.evaluated_permission)
  const confirmationResult = confirmationEvent?.result
  const resultIsError = resultEvent?.is_error === true

  if (permission === 'deny' || confirmationResult === 'deny') return 'denied'
  if (resultEvent) return resultIsError ? 'failed' : 'completed'
  if (permission === 'ask' && !confirmationEvent) return 'awaiting_approval'
  return 'running'
}
```

状态语义：

| lifecycle | 触发条件 | 展示含义 |
|---|---|---|
| `awaiting_approval` | `evaluated_permission=ask` 且无 confirmation | 工具尚未执行，等待外部确认。 |
| `running` | 自动允许，或 ask 后已有 allow confirmation，但尚无 result | 工具已可运行，正在等待结果。 |
| `failed` | 有 result 且 `is_error=true` | 工具执行失败。 |
| `denied` | 策略拒绝，或 confirmation `result=deny` | 工具不会执行。 |
| `completed` | 有 result 且非 error | 工具执行完成。 |

注意：

- `denied` 的优先级最高，因为策略拒绝或用户拒绝都意味着工具不会继续运行。
- `ask + allow confirmation + no result` 必须是 `running`，不是 `awaiting_approval`。
- `ask + no confirmation + no result` 才是 `awaiting_approval`。
- 缺少 `evaluated_permission` 时按 `allow` 兼容处理，除非事件显式带有 `requires_action=true`。

---

## 5. Tool batch 聚合

同一 model request bracket 下连续的多个 `tool_call` 可聚合为 `tool_batch`。batch 只影响展示，不改变每个 tool call 的原始事件关联。

聚合规则：

- `toolCounts` 按工具名统计，例如 `Read x2, Glob`。
- `executionMs` 取子调用最大值。
- `isError` 任一子调用为 error 即为 true。
- `lifecycle` 取最需要关注的子调用状态。

lifecycle 严重性排序：

```ts
const lifecyclePriority = {
  awaiting_approval: 0,
  running: 1,
  failed: 2,
  denied: 3,
  completed: 4,
}
```

batch lifecycle 使用数字最小的状态。这样只要 batch 内仍有工具等待确认，整行就显示 `awaiting approval`，避免被已完成工具掩盖。

---

## 6. UI 行为

### 6.1 Transcript 行

`tool_call` 行展示：

- 左侧 compact `Tool` badge。
- 主文本为工具名，后接输入摘要。
- 行尾 meta strip 依次展示：
  - `running` spinner，仅当 lifecycle 为 `running`。
  - approval chip，仅当 lifecycle 为 `awaiting_approval` 或 `denied`。
  - error badge，仅当 result error 或 entry error。
  - token usage。
  - execution/model duration。
  - relative timestamp。

Approval chip 规则：

| lifecycle | 展示 |
|---|---|
| `awaiting_approval` | accent chip，文案 `awaiting approval` / `等待批准`。 |
| `denied` | warning chip，带 prohibit/ban 图标，文案 `denied` / `已拒绝`。 |
| 其他 | 不渲染 approval chip。 |

`tool_batch` 行使用同一套 meta strip，并以 batch lifecycle 决定 chip。

### 6.2 详情面板

选中 `tool_call` 时，详情面板展示：

- lifecycle badge。
- Tool use input JSON。
- Tool result JSON；若没有 result，显示 `No result`。
- 如果存在 confirmation：
  - `result=allow` 显示确认结果。
  - `result=deny` 显示确认结果和 `deny_message`。

选中 Debug 中的 `user.tool_confirmation` 原始事件时，详情面板可直接显示原始 JSON。Transcript 不需要单独渲染该行。

### 6.3 Debug 模式

Debug 模式是事件审计视图，必须保留：

- 原始 `user.tool_confirmation` 事件。
- 原始 tool result 事件。
- 无法关联到 tool use 的孤立 confirmation/result。

孤立事件不应影响任何 `tool_call` lifecycle，但需要在 Debug 中可见，方便定位后端或导入数据问题。

---

## 7. 后端与事件发送边界

本文不新增公开 API。后端应继续按现有 session events 契约接受合法 client input events，其中 `user.tool_confirmation` 的最小校验为：

- `tool_use_id` 必填且非空。
- `result` 必须是 `allow` 或 `deny`。
- `deny_message` 如存在，必须是 string 或 null。

Session Detail 的 composer 或 action mutation 不应新增审批发送分支。Web UI 可发送的用户交互仍保持当前产品边界，例如用户消息和中断；tool confirmation 由外部运行客户端或调用方应用写入事件流。

---

## 8. 本仓库实现状态

当前前端实现位于 [sessionTraceModel.ts](/Users/arthur/GolandProjects/claude-api-server/web/src/features/managed-agents/sessions/sessionTraceModel.ts)、[sessionTraceRows.tsx](/Users/arthur/GolandProjects/claude-api-server/web/src/features/managed-agents/sessions/sessionTraceRows.tsx) 与 [SessionTracePanel.tsx](/Users/arthur/GolandProjects/claude-api-server/web/src/features/managed-agents/sessions/SessionTracePanel.tsx)，已按本文只读回放模型落地：

- `buildSessionTraceEntries` 在生成 Transcript 前预扫描完整事件流，建立 `toolResultsByUseId` 与 `toolConfirmationsByUseId`。
- Transcript 中 `user.tool_confirmation` 与 tool result 不独立成行，而是挂到对应 `tool_call.confirmationEvent` / `tool_call.resultEvent`。
- Debug 模式仍显示原始 `user.tool_confirmation` 与 tool result，便于审计孤立事件或关联错误。
- `sessionToolLifecycle` 支持字符串与对象形态的 `evaluated_permission` / `permission`，并按 `deny > result > ask-without-confirmation > running` 的顺序派生 lifecycle。
- `ask + allow confirmation + no result` 显示为 `running`；`ask + deny confirmation` 或策略 `deny` 显示为 `denied`；`ask + no confirmation` 显示为 `awaiting_approval`。
- tool 详情面板展示 confirmation JSON；拒绝事件会显示 `deny_message`。

相关测试在 `web/src/features/managed-agents/ManagedAgentsPage.test.tsx` 的 `folds tool confirmations into transcript tool rows while keeping debug audit events` 中覆盖。

## 9. 验收场景

| 场景 | 输入事件 | 期望展示 |
|---|---|---|
| 等待确认 | `tool_use(evaluated_permission=ask)`，无 confirmation/result | Transcript tool row 显示 `awaiting approval`。 |
| 确认后运行 | `ask` + `user.tool_confirmation(result=allow)`，无 result | tool row 显示 running spinner，不显示 awaiting chip。 |
| 用户拒绝 | `ask` + `user.tool_confirmation(result=deny, deny_message=...)` | tool row 显示 `denied`，详情可看到拒绝原因。 |
| 策略拒绝 | `tool_use(evaluated_permission=deny)` | 即使没有 confirmation，也显示 `denied`。 |
| 执行失败 | 有 result 且 `is_error=true` | tool row 显示 error badge，lifecycle 为 `failed`。 |
| 执行完成 | 有 result 且非 error | tool row 无 approval chip，lifecycle 为 `completed`。 |
| batch 等待 | 同一 bracket 多个 tool call，其中一个 awaiting | batch row 显示 `awaiting approval`。 |
| Transcript 过滤 | confirmation/result 原始事件存在 | Transcript 不出现独立 confirmation/result 行。 |
| Debug 审计 | confirmation/result 原始事件存在 | Debug 可看到原始事件 JSON。 |
| 只读边界 | 页面存在 awaiting tool call | 不出现 Allow/Deny 按钮，不调用 `events.send(user.tool_confirmation)`。 |

---

## 9. 实现注意

- 当前本仓库前端已把 `user.tool_confirmation` 纳入按 `tool_use_id` 关联的归一化模型；新增 permission 事件形态时应优先扩展现有 helper，而不是在行渲染处单独判断。
- 不要只从 `tool_use.evaluated_permission` 推断最终状态；用户拒绝必须来自 confirmation。
- 不要把 `user.tool_confirmation` 当普通 `user` 行渲染到 Transcript，否则会破坏官方两层模型。
- 不要在回放页补 Web 端审批按钮。若后续产品要支持 Web 审批，应另起设计，覆盖 mutation、权限门、乐观状态、失败回滚和 SSE 回灌。
- 自定义工具不受 permission policy 控制，但 custom tool use/result 仍应能在统一 tool 展示框架中回放。

---

## 10. 测试建议

前端单元测试应覆盖：

- `toolResultsByUseId` 与 `toolConfirmationsByUseId` 的关联键提取。
- `ask` 无 confirmation -> `awaiting_approval`。
- `ask` + allow confirmation -> `running`。
- `ask` + deny confirmation -> `denied`。
- `deny` permission -> `denied`。
- result error -> `failed`。
- result success -> `completed`。
- tool batch lifecycle 取最小 priority。
- Transcript 隐藏 confirmation/result，Debug 保留原始事件。

后端测试应继续覆盖：

- `user.tool_confirmation` 属于 client input event。
- 缺少 `tool_use_id` 返回 `invalid_request_error`。
- `result` 非 `allow`/`deny` 返回 `invalid_request_error`。
- `deny_message` 类型错误返回 `invalid_request_error`。

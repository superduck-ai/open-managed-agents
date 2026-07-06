# Session Tool 调用显示设计指南

---

## 1. 背景

Session Detail 的 tool 调用展示不是简单地把 event stream 原样逐条渲染。官方 Console 使用两层模型：

- **归一化层**：把 `tool_use`、`tool_result`、`user.tool_confirmation` 按 tool use id 合并为可显示的 entry。
- **渲染层**：根据 entry kind 路由到单工具行、批量工具行、普通消息行或详情面板。


- 本文关注 tool 调用如何显示。
- 权限确认文档关注 `evaluated_permission` / `user.tool_confirmation` 如何影响 lifecycle。
- 两者在 `ToolLifecycle`、`ApprovalChip`、Transcript 过滤规则上共享同一套前端派生模型。

---

## 2. 范围

### 2.1 包含

- `agent.tool_use` / `agent.mcp_tool_use` / `agent.custom_tool_use` 的统一显示。
- tool result 与 confirmation 按 id 挂到对应 tool call。
- 同一 model request bracket 内多个 tool call 聚合为 `tool_batch`。
- tool name 美化、input preview、running 动效、行尾元数据。
- `tool_call` 与 `tool_batch` 的详情面板结构。
- Transcript 与 Debug 的显示边界。
- 现有前端实现的对齐目标与测试建议。

### 2.2 不包含

- 后端 session event 存储 schema。
- Web 端 Allow / Deny 审批按钮。
- Claude Code runtime permission bridge。
- Minimap lane 坐标算法。详见 [session-detail-lane-timeline-design.md](session-detail-lane-timeline-design.md)。
- Tool result 内容的业务级解释或特定 MCP server 的自定义渲染。

---

## 3. 事件契约

### 3.1 Tool use

| 事件类型 | 关键字段 | 显示角色 |
|---|---|---|
| `agent.tool_use` | `id`, `tool_use_id`, `name`, `tool_name`, `input`, `evaluated_permission` | 预构建 agent toolset 或 Claude Code 内置工具调用。 |
| `agent.mcp_tool_use` | `id`, `tool_use_id`, `name`, `tool_name`, `input`, `evaluated_permission` | MCP 工具调用。 |
| `agent.custom_tool_use` | `id`, `custom_tool_use_id`, `name`, `input` | 自定义工具调用。 |

前端应通过统一 helper 读取 tool use id。推荐优先级：

```ts
tool_use_id ?? mcp_tool_use_id ?? custom_tool_use_id ?? id
```

`name` 与 `tool_name` 也应兼容读取。行内显示使用 tool display name，详情面板保留原始 payload。

### 3.2 Tool result

| 事件类型 | 关联字段 | 显示角色 |
|---|---|---|
| `agent.tool_result` | `tool_use_id` | agent toolset result。 |
| `agent.mcp_tool_result` | `mcp_tool_use_id` 或 `tool_use_id` | MCP result。 |
| `user.tool_result` | `tool_use_id` | 调用方写回的 tool result。 |
| `user.custom_tool_result` | `custom_tool_use_id` | 自定义工具 result。 |

result 的 `is_error=true` 影响 `ToolLifecycle` 与错误徽章。result body 不在行内展开，只在详情面板展示。

### 3.3 Tool confirmation

`user.tool_confirmation` 不作为 Transcript 中的独立行展示。它只影响对应 tool call lifecycle，并在 Debug 中保留原始事件。

```ts
interface UserToolConfirmationEvent {
  type: 'user.tool_confirmation'
  tool_use_id: string
  result: 'allow' | 'deny'
  deny_message?: string | null
}
```

---

## 4. 派生数据模型

当前前端已有 `ToolCallEntry`、`ToolBatchEntry`、`ToolLifecycle` 等类型。目标模型应保持以下语义：

```ts
type ToolLifecycle =
  | 'awaiting_approval'
  | 'running'
  | 'failed'
  | 'denied'
  | 'completed'

interface ToolCallEntry {
  kind: 'tool_call'
  id: string
  event: SessionEvent
  name: string
  input: unknown
  inputPreview?: string
  resultEvent?: SessionEvent
  confirmationEvent?: UserToolConfirmationEvent
  lifecycle: ToolLifecycle
  executionMs?: number
  isError?: boolean
  bracketId?: string
  sourceEventIds: string[]
}

interface ToolBatchEntry {
  kind: 'tool_batch'
  id: string
  calls: ToolCallEntry[]
  toolCounts: Array<{ name: string; count: number }>
  lifecycle: ToolLifecycle
  executionMs?: number
  isError?: boolean
  bracketId?: string
  sourceEventIds: string[]
}
```

设计约束：

- `tool_call.sourceEventIds` 至少包含 tool use event id；若有关联 result 或 confirmation，也应包含对应 id。
- `tool_batch.sourceEventIds` 是所有子 call 的 source ids 并集。
- `tool_batch` 不改变每个 call 的详情数据，只改变列表层显示。
- Debug 视图仍以原始事件为审计基础；Transcript 视图使用归一化 entry。

---

## 5. 归一化流程

### 5.1 预扫描索引

生成 Transcript entries 前先扫描完整事件列表：

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

主循环中：

- 遇到 tool use 事件，生成 `tool_call`。
- 遇到 tool result 或 `user.tool_confirmation`，Transcript 直接跳过。
- Debug 不跳过任何原始事件。

### 5.2 Tool call 生成

每个 tool use entry 需要派生：

- `name`：原始 tool name，用于详情与 batch counting。
- `displayName`：渲染时通过 tool name 美化函数生成。
- `input`：`sessionToolUseInput(event)` 的原始对象。
- `inputPreview`：通过字段优先级提取的单行预览。
- `resultEvent`：从 `toolResultsByUseId` 读取。
- `confirmationEvent`：从 `toolConfirmationsByUseId` 读取。
- `lifecycle`：由 permission、confirmation、result 共同决定。
- `executionMs`：若 result 与 use 都有时间戳，使用差值；否则为空。
- `isError`：result `is_error=true` 或 entry-level error。

### 5.3 Multi-thread 协议边界

本仓库的 session detail 会同时消费主线程与子线程事件。tool use 的线程归属必须先由后端按 Anthropic 协议输出正确，前端不通过隐藏、去重或改写 owner 来掩蔽后端接口问题。

后端协议规则优先：

- primary session events 只返回 coordinator 自身事件、thread lifecycle/message coordination，以及需要客户端动作的子线程阻塞事件。
- 普通子线程 `agent.tool_use` / `agent.mcp_tool_use` / result 不应出现在 primary；它们应只属于对应 thread events。
- 需要 cross-post 的子线程 tool use/custom tool use 必须在 primary copy 上带 `session_thread_id`。thread 自己的 copy 可以不带，响应层可按当前 thread id 补齐用于展示归属。
- 需要 cross-post 的 result/confirmation 同样必须带 `session_thread_id`。若 primary 中存在无归属 `tool_result`，但同一 `tool_use_id` 已在子线程 owner copy 中出现，后端响应层应把它视为 orphan projection 并过滤。
- 若 primary 返回了无 `session_thread_id` 的子线程工具事件，这是后端契约错误，应通过后端事件复制、响应过滤和 API 测试修复。

前端 Transcript 按事件 payload 和已知 thread 字段如实渲染。若 API 返回了错误归属或重复投影，UI 可以把问题暴露出来，方便定位后端协议偏差；不要在前端增加针对这类异常 payload 的隐藏规则。

---

## 6. Batch 合并

同一 model request bracket 内的连续片段可以同时包含 message 与 tool call。合并规则：

1. 先按 `bracketId` 收集连续片段。
2. 片段中的 message entry 保持独立行。
3. 片段中 tool call 数量为 1 时保持 `tool_call`。
4. 片段中 tool call 数量大于等于 2 时合并为 `tool_batch`。

`tool_batch` 派生字段：

| 字段 | 规则 |
|---|---|
| `toolCounts` | 按原始 `name` 分组计数。 |
| `executionMs` | 取子 call 最大值。 |
| `isError` | 任一子 call error 即为 true。 |
| `lifecycle` | 取最需要关注的状态。 |
| `usage` / `inferenceMs` | 若 bracket 内没有 message，可归 batch；否则归 message。 |

lifecycle 优先级：

```ts
const lifecyclePriority = {
  awaiting_approval: 0,
  running: 1,
  failed: 2,
  denied: 3,
  completed: 4,
} satisfies Record<ToolLifecycle, number>
```

取 priority 最小的 lifecycle。这样 batch 中只要存在等待确认或运行中的工具，列表行就优先提示。

---

## 7. Tool 行渲染

### 7.1 行结构

`tool_call` 和 `tool_batch` 都使用统一 HeaderRow 外壳：

```text
[左: tool icon 固定宽] [中: 名称与 input preview, truncate] [右: MetaStrip]
```

要求：

- 整行可选中，使用 `aria-pressed` 表示 selected。
- 行根节点带稳定 `data-event-id` 与 `data-entry-kind`，支持 minimap seek、测试和复制。
- 列表层始终单行折叠；完整 input/result 不在行内展开。
- hover/selected 样式不能改变行高，避免时间轴跳动。

### 7.2 ToolCallRow

展示内容：

```text
ToolIcon  {displayToolName(name)}  {inputPreview}  {MetaStrip}
```

规则：

- 左侧固定宽度，使用 compact tool icon。
- 中间主文本是美化后的工具名。
- `inputPreview` 作为灰色 suffix，和工具名同行显示。
- `lifecycle === 'running'` 时，工具名与 suffix 使用同步脉动 badge。

### 7.3 ToolBatchRow

展示内容：

```text
ToolIcon  Read, Edit ×3, Bash  {MetaStrip}
```

规则：

- 文案由 `toolCounts` 生成。
- count 大于 1 时显示 `工具名 ×N`。
- batch 行不展示 input preview。
- MetaStrip 使用 batch 聚合后的 lifecycle、executionMs、isError、usage。

---

## 8. 工具名美化

官方行为只做轻量转换：

```ts
function displayToolName(name: string) {
  return name
    .replace(/^(agent_|mcp_|computer_)/, '')
    .split('_')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}
```

设计约束：

- 只剥离一次 `agent_`、`mcp_`、`computer_` 前缀。
- 只按 `_` 分词。
- 不拆 camelCase。
- 不清理重复下划线产生的空段。
- 详情面板保留原始 `name` / `tool_name`。

示例：

| 原始 name | 显示 |
|---|---|
| `Bash` | `Bash` |
| `WebSearch` | `WebSearch` |
| `agent_read_file` | `Read File` |
| `mcp__github__search_repositories` | ` Github  Search Repositories` |

最后一个示例中的前导空格与双空格来自官方算法。若后续产品决定做本地增强，应另起视觉设计，不要悄悄改变官方对齐目标。

---

## 9. Input Preview

`inputPreview` 从 tool input 中挑信息密度最高的字段，默认截断到 60 字符：

| 优先级 | 字段 | 处理 |
|---|---|---|
| 1 | `description` | `trim` |
| 2 | `command` | `trim` + 压缩空白 |
| 3 | `file_path` | 原样保留路径 |
| 4 | `path` | 原样 |
| 5 | `query` | `trim` |
| 6 | `url` | 去掉 `http://` / `https://` 后截断 |
| 7 | `pattern` | `trim` |
| 8 | `prompt` | `trim` + 压缩空白 |
| 9 | `text` | `trim` + 压缩空白 |
| 10 | fallback | 第一个非空且长度不超过 200 的 string 字段 |

约束：

- `input` 不是 object 时不生成 preview。
- preview 仅用于列表行，不替代详情面板中的完整 JSON。
- `file_path` 与 `path` 不压缩空白，避免破坏路径可读性。
- URL 只去协议，不去 host/path。
- fallback 必须稳定，按对象原始枚举顺序取第一个符合条件的字符串。

---

## 10. MetaStrip

tool 行尾按固定顺序渲染：

| 顺序 | 元素 | 条件 |
|---|---|---|
| 1 | Running chip | `lifecycle === 'running'` |
| 2 | Approval chip | `lifecycle === 'awaiting_approval'` 或 `denied` |
| 3 | Error badge | `isError === true` |
| 4 | Token usage | usage 存在 |
| 5 | Duration | `executionMs` 或 `inferenceMs` 存在 |
| 6 | Timestamp | `processedAtMs` 有效 |

交互：

- token usage hover 展示 input、cache read、cache creation、output 明细。
- duration hover 可展示 model inference 与 tool execution 的拆分。
- timestamp hover 展示绝对时间。
- MetaStrip 不参与行主文本截断，右侧宽度稳定。

---

## 11. Running 动效

`lifecycle === 'running'` 时，工具名和 suffix 使用同相位脉动 badge：

```ts
const delay = -(performance.now() % pulsePeriodMs)
```

设计理由：

- 多个 running tool 同时出现时，动画相位一致，视觉更安静。
- `inputPreview` 与工具名同步脉动，表示二者属于同一个 running call。
- 非 running 状态回退为纯文本，避免 completed 历史行持续吸引注意力。

实现注意：

- 动画只在 client 端计算，不应影响 SSR/hydration 稳定性。
- 尊重 `prefers-reduced-motion`，关闭或弱化脉动。
- 动效不改变文本布局尺寸。

---

## 12. 详情面板

### 12.1 Tool call detail

选中 `tool_call` 时，右侧详情面板展示：

- approval chip：仅 `awaiting_approval` 或 `denied` 时显示。
- `Tool use`：格式化 JSON，来自 `sessionToolUseInput(event)`。
- `Tool result`：若有关联 result，显示 result 文本或 JSON。
- `No result`：若没有 result，显示斜体空状态。
- error result：使用 error 背景或边框强调。

result 渲染：

- 若 result content 可解析为 JSON，格式化为 JSON code block。
- 否则按 text 展示。
- 最大行数应有限制，例如 300 行，避免详情面板卡顿。

### 12.2 Tool batch detail

选中 `tool_batch` 时，右侧详情面板展示 batch 专属 accordion：

- 每个 call 一段 `CallSection`。
- section header 展示 `inputPreview ?? displayToolName(name)`。
- header 同时展示该 call 的 approval chip 与 execution duration。
- 展开后分别展示该 call 的 `Tool use` 与 `Tool result`。
- 支持键盘 `j` / `k` 或上下箭头在 batch 内切换焦点 call。

### 12.3 Confirmation raw detail

Transcript 不显示独立 `user.tool_confirmation` 行。Debug 视图中选中原始 confirmation 时，可以展示：

```json
{
  "result": "allow",
  "deny_message": "optional reason"
}
```

这属于 Debug 审计能力，不是 Transcript 常规入口。

---

## 13. Live 增量

tool call 行本身不做 token 级 streaming preview。实时增量主要作用于 message/thinking 行：

- stream delta 更新 message entry 的 preview。
- tool use 在事件到达后一次性生成 `tool_call`。
- tool result 到达后，归一化层更新对应 call 的 `resultEvent` 与 lifecycle。

实现要求：

- list pagination 与 SSE stream 合并后必须重新跑 tool result / confirmation 关联。
- 不完整 streaming event 清理后，不应留下 orphan running tool row。
- 同一 event id 的新版本应替换旧版本，避免 duplicated row。

---

## 14. UI 与可访问性

- 行文本必须单行 truncate，不能挤压 MetaStrip。
- `tool_batch` 的工具列表太长时，主文本截断；详情面板提供完整 call 列表。
- 所有 icon-only control 需要 tooltip 或 accessible label。
- 颜色不能作为唯一状态表达；running、approval、error 需要图标或文本辅助。
- selected row 使用 `aria-pressed` 或等价语义。
- Debug 与 Transcript tabs 保持同一 selected event 状态，切换视图时尽量定位到同源 event。

---

## 15. 本仓库实现状态

当前主要实现文件是 [sessionTraceModel.ts](/Users/arthur/GolandProjects/claude-api-server/web/src/features/managed-agents/sessions/sessionTraceModel.ts)，行渲染在 [sessionTraceRows.tsx](/Users/arthur/GolandProjects/claude-api-server/web/src/features/managed-agents/sessions/sessionTraceRows.tsx)，详情面板在 [SessionTracePanel.tsx](/Users/arthur/GolandProjects/claude-api-server/web/src/features/managed-agents/sessions/SessionTracePanel.tsx)。

已实现对齐点：

- `SessionTraceEntry` / `ToolCallEntry` 保存 `resultEvent` 与 `confirmationEvent`；选择定位的 source id 集合同时包含 tool use、result、confirmation。
- `buildSessionTraceEntries` 先对完整事件流建立 `toolResultsByUseId` 与 `toolConfirmationsByUseId`，Transcript 再把 result / confirmation 折回 `tool_call`；Debug 继续逐条显示原始事件。
- `sessionToolUseId` / `sessionToolResultToolUseId` 兼容 `tool_use_id`、`mcp_tool_use_id`、`custom_tool_use_id` 与 `id`。
- `sessionToolDisplayName` 使用官方轻量美化：只剥一次 `agent_` / `mcp_` / `computer_` 前缀，只按 `_` 分词。
- `sessionToolDisplayText` 按本文字段优先级生成 60 字符 input preview，`file_path` / `path` 保留原始路径。
- `mergeToolCallBatches` 以连续 bracket 片段为单位聚合，允许片段内混有 agent message；message 保持独立，多个 tool call 折成 `tool_batch`。
- `ToolCallRow` / `ToolBatchRow` 保持三段式布局和 MetaStrip 顺序；batch summary 使用 `×N`。
- `ToolCallDetailContent` / `BatchDetailPanel` 展示每个 call 的 input、confirmation、result；Debug 视图保留原始 tool result 与 `user.tool_confirmation`。

已覆盖测试：

- `web/src/features/managed-agents/ManagedAgentsPage.test.tsx` 中的 `folds tool confirmations into transcript tool rows while keeping debug audit events` 覆盖 `awaiting_approval`、`ask + allow -> running`、`deny`、Debug 审计保留，以及同 bracket 夹 message 的 batch 聚合。

---

## 16. 验收场景

| 场景 | 输入 | 期望 |
|---|---|---|
| 单 tool call | 一个 `agent.tool_use`，无 result | Transcript 显示一条 `tool_call`，详情为 `No result`。 |
| tool result 合并 | tool use + matching result | Transcript 仍只有一条 tool 行，详情显示 result。 |
| confirmation 合并 | tool use + `user.tool_confirmation` | Transcript 不出现 confirmation 独立行，tool lifecycle 受 confirmation 影响。 |
| result error | result `is_error=true` | 行尾显示 error badge，详情 result 使用 error 样式。 |
| MCP 工具 | `mcp__weather_service__get_weather` | 行内显示美化名称和 input preview；详情保留原始 name。 |
| input preview | Bash command / file_path / query / url | 按字段优先级显示正确 suffix。 |
| batch 合并 | 同一 bracket 内 3 个 tool call | Transcript 显示一条 `tool_batch`，文案类似 `Read, Edit ×2`。 |
| batch detail | 选中 `tool_batch` | 右侧展示每个 call 的独立 input/result。 |
| running 动效 | lifecycle `running` | 工具名与 suffix 脉动，MetaStrip 显示 Running。 |
| Debug 审计 | 原始 result/confirmation 存在 | Debug 能看到原始事件 JSON。 |
| 跨线程重复 | 同一 `tool_use_id` 被主线程与子线程同时投影 | Transcript 同一可见流不重复显示，Debug 保留全部。 |

---

## 17. 测试建议

前端单元测试：

- `toolResultsByUseId` 支持 `tool_use_id` / `mcp_tool_use_id` / `custom_tool_use_id`。
- `user.tool_confirmation` 不生成 Transcript 行。
- `inputPreview` 覆盖 `description`、`command`、`file_path`、`path`、`query`、`url`、`pattern`、`prompt`、`text`、fallback。
- `displayToolName` 只剥离一次前缀，不拆 camelCase，不清理重复下划线。
- batch lifecycle 取最小 priority。
- batch summary 使用 `×N`。
- Debug 保留 raw result/confirmation。

组件测试：

- `ToolCallRow` 渲染 icon、display name、suffix、MetaStrip。
- `ToolBatchRow` 不渲染 input preview，只渲染 summary。
- `ToolUseEventDetail` 展示 input JSON、result JSON/text、No result、error 样式。
- `ToolBatchEventDetail` 展示每个 call 的 accordion section。
- running 状态有可访问文本，reduced motion 下不依赖动画表达状态。

集成测试：

- 使用 MCP weather agent 跑子代理调用三个城市时，后端只有三次 tool execution，Transcript 中同一可见流不重复显示同一 `tool_use_id`。
- 列表分页历史事件 + SSE 新事件合并后，tool result 能回填到已有 tool call。
- 切换 Transcript / Debug 后，选中 entry 与详情面板保持可理解的对应关系。

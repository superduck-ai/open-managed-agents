# 会话工具审批与状态展示

## 配置与调用状态

工具权限与工具调用状态是两类信息。
智能体的 `tools` 配置决定自动执行、每次询问或拒绝执行。
历史事件决定本次调用的等待、执行、完成、失败或拒绝状态。
配置规则见[权限策略](../../be/permission-policies.md)。

控制台从会话及各线程的最新状态读取待处理项。
`stop_reason.type=requires_action` 时，`event_ids` 指定需要用户处理的调用。
普通工具使用允许和拒绝按钮。
`AskUserQuestion` 使用问卷。
分别提交 `user.tool_confirmation` 和 `user.custom_tool_result`。
页面布局边界见[会话时间线设计](session-detail-lane-timeline-design.md)。

## 线程与事件归属

| 接口                                                           | 历史范围                               |
| -------------------------------------------------------------- | -------------------------------------- |
| `/v1/sessions/{session_id}/events`                             | 主线程视图及需要客户端处理的子线程镜像 |
| `/v1/sessions/{session_id}/threads/{session_thread_id}/events` | 对应子线程的历史                       |

普通子工具及结果归属于子线程。
需要审批或自定义结果时，后端可将子调用复制到主线程。
镜像必须携带 `session_thread_id`，表示来源子线程。
该字段不改变镜像事件的主线程归属。
子接口已通过 URL 指定线程，原始载荷可以省略重复线程字段。
响应补齐字段只方便读取，不改变存储归属。

后端过滤主线程中无来源标识的孤立子事件副本。
同一规则适用于调用、确认和结果。
这类归属错误由接口和测试处理，前端不根据相似内容隐藏事件。

## 工具调用契约

| 事件类型                | 关键字段                                      | 用途           |
| ----------------------- | --------------------------------------------- | -------------- |
| `agent.tool_use`        | `id`、`name`、`input`、`evaluated_permission` | 内置工具调用   |
| `agent.mcp_tool_use`    | `id`、`name`、`input`、`evaluated_permission` | MCP 工具调用   |
| `agent.custom_tool_use` | `id`、`name`、`input`                         | 自定义工具调用 |

`evaluated_permission` 表示后端对本次调用的评估。
前端按下表归一化：

| 原始值                                 | 内部值  | 含义     |
| -------------------------------------- | ------- | -------- |
| `ask`、`always_ask`、`requires_action` | `ask`   | 等待确认 |
| `allow`、`always_allow`                | `allow` | 可以执行 |
| `deny`、`denied`                       | `deny`  | 拒绝执行 |

自定义工具不使用普通工具的权限策略，但复用调用展示。

## 确认与结果契约

普通工具确认使用以下事件：

```ts
interface UserToolConfirmationEvent {
  type: "user.tool_confirmation";
  tool_use_id: string;
  result: "allow" | "deny";
  deny_message?: string | null;
  session_thread_id?: string;
}
```

`tool_use_id` 必须是对应工具事件的公开 ID。
`result=allow` 表示允许执行，`result=deny` 表示拒绝执行。
`deny_message` 如存在，必须是字符串或 `null`。
子工具确认必须携带对应子线程的 `session_thread_id`。
原始 ID 和镜像 ID 都必须关联同一个待处理请求。

未知 ID、错误线程或已处理请求返回输入冲突。
失败确认不得保存为成功事件。

| 结果事件                  | 关联字段             |
| ------------------------- | -------------------- |
| `user.tool_result`        | `tool_use_id`        |
| `agent.tool_result`       | `tool_use_id`        |
| `agent.mcp_tool_result`   | `mcp_tool_use_id`    |
| `user.custom_tool_result` | `custom_tool_use_id` |

`is_error=true` 表示执行失败。
原始载荷保留下载、流式复制和外部错误等详细信息。
回复记录视图使用状态和可读摘要。
结果与确认遵循对应调用的线程归属。

## 统一调用身份

主线程镜像和子原始事件保留各自公开 `id`。
镜像通过 `original_event_id` 指向原始事件。
原始事件通过 `primary_event_id` 指向镜像。

事件合并层先建立两套 ID 到原始调用的映射。
内部 `toolCallId` 用于工具记录去重，以及确认和结果关联。
内部 `pendingToolCallIds` 用于待处理状态匹配。
两份事件都已加载时，选择原始事件。
只加载镜像时，仍提供审批入口。
提交确认继续使用选中事件的公开 ID。

旧子历史读取补齐可推导的关联，不改写存储历史。
独立调用即使线程、文件、输入和时间相同，也保持独立。

前端分别读取会话与各子线程的最新状态。
子状态包括 `session.thread_status_idle`。
父线程运行时，子线程待审批项仍保留。
对应确认、结果或后续线程状态清除待处理项。
会话终止或删除时，清除全部待处理项。

## 归一化展示

生成回复记录前，先扫描完整事件流。
建立结果索引和确认索引：

```ts
const toolResultsByUseId = new Map<string, SessionEvent>();
const toolConfirmationsByUseId = new Map<string, UserToolConfirmationEvent>();
```

索引键使用统一调用身份。
工具记录通过 `resultEvent` 和 `confirmationEvent` 保存关联事件。
回复记录视图不单独显示已经关联的确认和结果行。
调试视图保留原始事件及无法关联的孤立事件。
孤立事件不改变其他调用的状态。

```ts
type ToolLifecycle =
  "awaiting_approval" | "running" | "failed" | "denied" | "completed";

interface ToolCallEntry {
  kind: "tool_call";
  name: string;
  input: unknown;
  resultEvent?: SessionEvent;
  confirmationEvent?: UserToolConfirmationEvent;
  lifecycle: ToolLifecycle;
  bracketId?: string;
}
```

## 调用状态

按以下优先级计算状态：

1. 策略或用户拒绝时，状态为 `denied`。
2. 已有结果时，根据 `is_error` 选择 `failed` 或 `completed`。
3. 需要确认且没有确认时，状态为 `awaiting_approval`。
4. 其他情况为 `running`。

| 条件                           | 状态                | 中文含义 |
| ------------------------------ | ------------------- | -------- |
| `ask`，尚无确认                | `awaiting_approval` | 等待批准 |
| 自动允许，或已允许但尚无结果   | `running`           | 执行中   |
| 结果的 `is_error=true`         | `failed`            | 执行失败 |
| 策略拒绝或确认的 `result=deny` | `denied`            | 已拒绝   |
| 已有结果且没有错误             | `completed`         | 已完成   |

缺失权限字段时，按已有允许策略兼容。
若事件明确包含 `requires_action=true`，则按待处理调用展示。
历史状态用于回放，不能单独决定是否显示当前操作卡。
当前操作卡还必须匹配最新待处理集合。
确认和结果到达后，立即从该集合移除对应调用。

## 工具批次

同一模型请求中连续的调用可合并为 `tool_batch`。
批次只改变展示，每个调用仍保留自己的事件关联。

- `toolCounts` 按工具名称统计。
- `executionMs` 取子调用最大值。
- `isError` 在任一子调用失败时为 `true`。
- 批次状态取最需要处理的子调用状态。

状态优先级为等待批准、执行中、失败、拒绝、完成：

```ts
const lifecyclePriority = {
  awaiting_approval: 0,
  running: 1,
  failed: 2,
  denied: 3,
  completed: 4,
};
```

只要批次仍有待审批调用，整行就显示等待批准。

## 页面交互

工具行显示工具名称、输入摘要和状态。
执行中显示加载图标。
等待批准和拒绝状态显示对应标签。
错误结果显示错误标记。
其他信息包括令牌用量、耗时和相对时间。
工具批次使用相同信息栏。

详情面板显示状态、输入 JSON、结果 JSON 和确认 JSON。
没有结果时，显示无结果提示。
拒绝确认还显示 `deny_message`。
调试视图允许查看原始确认、结果及孤立事件。

右侧操作卡提供允许、拒绝或问卷交互。
等待处理时，左侧普通消息输入框保持可见并禁用。
历史调用已完成后，不再显示操作卡。
父线程运行不应隐藏子线程审批入口。

工具配置表显示配置权限，不从历史确认反推配置。
主线程按会话固定的智能体 ID 和版本读取配置。
子线程详情显示该线程自己的配置快照。
内置与 MCP 工具复用智能体详情页的权限规则。
规则包括首个配置优先、`default_config`、`enabled=false` 和已有默认权限。

历史权限、确认和结果只表示本次调用。
配置暂时不可读或自定义工具无配置权限时，才使用已观察结果作为兼容信息。
配置为每次询问的工具，在历史调用完成后仍显示每次询问。

## 问卷提交

`AskUserQuestion` 发布为 `agent.custom_tool_use`。
控制台提交 `user.custom_tool_result`。

1. 使用待处理集合所引用的公开 `custom_tool_use_id`。
2. 子线程请求携带对应 `session_thread_id`。
3. 接受答案时，设置 `is_error=false`。
4. 将答案对象 JSON 放入单个文本内容块。
5. 使用完整 `question` 文本作为答案键。

拒绝时，设置 `is_error=true`，无需 `content`。
后端从私有映射读取原始输入，并设置 Claude Code 的 `updatedInput.answers`。
公开 `user.tool_confirmation` 不增加 `updated_input` 或 `answers` 字段。

## 实现与验收

实现位于 `sessionTraceModel.ts`、`sessionTraceRows.tsx`、`SessionRequiresActionCard.tsx`、`SessionDetailPage.tsx` 和 `SessionTracePanel.tsx`。
新增事件形态时，扩展现有归一化和关联函数。
用户拒绝必须读取确认结果，不能只读取初始权限。

| 场景                   | 期望结果                                     |
| ---------------------- | -------------------------------------------- |
| 普通工具等待确认       | 显示等待批准及允许、拒绝按钮，普通输入框禁用 |
| 用户允许               | 发送允许确认，工具转为执行中                 |
| 用户拒绝               | 发送拒绝确认，工具显示已拒绝及原因           |
| 问卷提交               | 按完整问题文本关联答案，发送自定义结果       |
| 策略拒绝               | 无用户确认也显示已拒绝                       |
| 结果失败               | 显示失败状态及错误标记                       |
| 结果成功               | 显示已完成，不显示审批标签                   |
| 批次中一项等待         | 整批显示等待批准                             |
| 父线程运行             | 子线程仍提供审批入口                         |
| 并行子线程             | 处理一个线程不清除另一个线程的待处理项       |
| 原始与镜像调用同时加载 | 只显示一条工具记录，确认与结果正确关联       |
| 独立调用内容相同       | 分别显示，不按内容合并                       |
| 刷新历史               | 已完成调用不再出现重复执行中记录             |
| 调试视图               | 保留原始确认、结果和孤立事件                 |

组件测试覆盖操作卡和权限展示。
`sessionApproval.test.tsx` 覆盖子线程与并行审批状态。
`sessionToolIdentity.test.ts` 覆盖两套 ID、结果关联及独立调用。
真实工作进程验证见[子智能体验证说明](../../../../.agents/skills/verify-be/features/subagents.md)。

# Anthropic Create Session API 响应契约核验

核验日期：2026-08-05

## 结论

Anthropic Managed Agents 的 Create Session 接口为 `POST /v1/sessions`，请求需携带 `anthropic-beta: managed-agents-2026-04-01`。成功响应状态为 **200**，不是 201；响应体是完整的 `BetaManagedAgentsSession`，不是只含 `id`、`status` 的创建结果。

官方 TypeScript SDK 内部请求路径是 `/v1/sessions?beta=true`，其中 `?beta=true` 是 SDK 生成代码的传输细节；公开 API 路由仍记作 `POST /v1/sessions`。SDK 会自动追加上述 beta header。

最需要关注的兼容点是：

- `agent` 必须是创建时解析完成的完整 Agent 快照，不是 Agent ID 字符串或简化引用。
- 除 `deployment_id` 外，根对象字段在官方 SDK 类型中均为必有字段。
- `archived_at`、`title` 是必有但可为 `null`，不能仅因值为空就省略。
- `metadata`、`outcome_evaluations`、`resources`、`stats`、`usage`、`vault_ids` 即使为空，也仍属于根对象必有字段。
- `status` 当前只允许 `rescheduling | running | idle | terminated`，不包含旧式的 `pending`。
- `agent.model.effort` 在响应中是 `{ "type": "..." }` 对象，而不是请求侧也允许的裸字符串。
- `initial_events` 不在创建响应中回显；非空 `initial_events` 会让新 Session 直接以 `running` 状态返回，省略或传空数组时通常是 `idle`。

## HTTP 契约

| 项目 | 官方契约 |
| --- | --- |
| 方法与公开路由 | `POST /v1/sessions` |
| Beta header | `anthropic-beta: managed-agents-2026-04-01` |
| SDK 内部路径 | `/v1/sessions?beta=true` |
| 成功状态 | `200 OK` |
| 成功响应类型 | `BetaManagedAgentsSession` |

依据：Anthropic [Create Session API Reference](https://platform.claude.com/docs/en/api/beta/sessions/create)、[Start a session 指南](https://platform.claude.com/docs/en/managed-agents/sessions)，以及官方 TypeScript SDK 的 [Create Session 请求实现](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/sessions.ts#L129-L155)。

## 响应根对象

下表的“必有”以官方 TypeScript SDK 由 OpenAPI 自动生成的 `BetaManagedAgentsSession` 类型为准。

| 字段 | JSON 类型 | 必有 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` | 是 | Session ID。 |
| `agent` | `object` | 是 | 已解析的完整 Agent 快照，见下文。 |
| `archived_at` | `string \| null` | 是 | RFC 3339 时间戳；未归档时为 `null`。 |
| `created_at` | `string` | 是 | RFC 3339 时间戳。 |
| `environment_id` | `string` | 是 | 关联的 Environment ID。 |
| `metadata` | `object<string,string>` | 是 | 字符串键值元数据；无数据时应为空对象。 |
| `outcome_evaluations` | `array<object>` | 是 | Outcome 评估状态；无数据时应为空数组。 |
| `resources` | `array<object>` | 是 | Session 资源联合类型；无数据时应为空数组。 |
| `stats` | `object` | 是 | Session 时间统计；对象内部字段可省略。 |
| `status` | `string enum` | 是 | `rescheduling`、`running`、`idle` 或 `terminated`。 |
| `title` | `string \| null` | 是 | 人类可读标题。 |
| `type` | `"session"` | 是 | 固定判别字段。 |
| `updated_at` | `string` | 是 | RFC 3339 时间戳。 |
| `usage` | `object` | 是 | 累计 token 使用量；对象内部字段可省略。 |
| `vault_ids` | `array<string>` | 是 | 创建时关联的 Vault ID；未提供时为空数组。 |
| `deployment_id` | `string \| null` | 否 | 由 Deployment reference 创建时为 Deployment ID，否则为 `null`；官方 TS 类型还允许字段省略。 |

依据：官方 SDK 的 [`BetaManagedAgentsSession`](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/sessions.ts#L625-L694)。

## `agent` 快照

`agent` 是 Session 创建时解析并固化的完整 Agent 定义。若请求使用 `agent_with_overrides`，响应中的快照反映 override 应用后的最终配置，但 `id` 和 `version` 仍指向基础 Agent 及其版本。官方指南对此有明确说明：[Override agent configuration for a session](https://platform.claude.com/docs/en/managed-agents/sessions#override-agent-configuration-for-a-session)。

`agent` 的下列字段均为必有：

| 字段 | JSON 类型 | 说明 |
| --- | --- | --- |
| `id` | `string` | Agent ID。 |
| `description` | `string \| null` | Agent 描述。 |
| `mcp_servers` | `array<MCPServer>` | 已解析的 MCP Server 列表。 |
| `model` | `ModelConfig` | 已解析的模型配置。 |
| `multiagent` | `SessionMultiagentCoordinator \| null` | 多 Agent 协调拓扑；非协调 Agent 为 `null`。 |
| `name` | `string` | Agent 名称。 |
| `skills` | `array<AnthropicSkill \| CustomSkill>` | 已解析并固定版本的 Skill 列表。 |
| `system` | `string \| null` | 系统提示词。 |
| `tools` | `array<AgentToolset \| MCPToolset \| CustomTool>` | 已解析的工具配置。 |
| `type` | `"agent"` | 固定判别字段。 |
| `version` | `number` | 使用的 Agent 版本。 |

依据：官方 SDK 的 [`BetaManagedAgentsSessionAgent`](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/sessions.ts#L696-L733)。

### `agent.mcp_servers[]`

每项均为必有字段：

```json
{
  "name": "string",
  "type": "url",
  "url": "string"
}
```

### `agent.model`

| 字段 | JSON 类型 | 必有 | 说明 |
| --- | --- | --- | --- |
| `id` | `string` | 是 | 模型标识符。官方类型保留已知模型枚举，同时允许未来字符串值。 |
| `effort` | `object` | 否 | `{ "type": "low" \| "medium" \| "high" \| "xhigh" \| "max" }`。 |
| `speed` | `"standard" \| "fast"` | 否 | 推理速度模式。 |

请求参数中的 `effort` 可以是裸字符串或对象，但响应类型只定义为带 `type` 的对象；兼容实现不应把请求形式原样透传进响应。

### `agent.skills[]`

Skill 是带判别字段的联合类型，两种结构的字段均为必有：

```text
{ skill_id: string, type: "anthropic", version: string }
| { skill_id: string, type: "custom", version: string }
```

### `agent.tools[]`

工具联合类型如下：

```text
AgentToolset = {
  type: "agent_toolset_20260401",
  configs: Array<{
    enabled: boolean,
    name: "bash" | "edit" | "read" | "write" | "glob" | "grep" | "web_fetch" | "web_search",
    permission_policy: { type: "always_allow" | "always_ask" }
  }>,
  default_config: {
    enabled: boolean,
    permission_policy: { type: "always_allow" | "always_ask" }
  }
}

MCPToolset = {
  type: "mcp_toolset",
  mcp_server_name: string,
  configs: Array<{
    enabled: boolean,
    name: string,
    permission_policy: { type: "always_allow" | "always_ask" }
  }>,
  default_config: {
    enabled: boolean,
    permission_policy: { type: "always_allow" | "always_ask" }
  }
}

CustomTool = {
  type: "custom",
  name: string,
  description: string,
  input_schema: {
    type: "object",
    properties?: object | null,
    required?: string[] | null,
    ...其他 JSON Schema 字段
  }
}
```

### `agent.multiagent`

值为 `null`，或：

```text
{
  type: "coordinator",
  agents: SessionThreadAgent[]
}
```

每个 `SessionThreadAgent` 也是完整快照，包含必有的 `id`、`description`、`mcp_servers`、`model`、`name`、`skills`、`system`、`tools`、`type: "agent"`、`version`；为避免递归，线程 Agent 不再重复 `multiagent` 字段。

以上嵌套依据：官方 SDK 的 [Agent 响应类型](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/agents/agents.ts#L207-L303)、[Skill 与 Custom Tool 类型](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/agents/agents.ts#L449-L533)、[MCP 与模型配置类型](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/agents/agents.ts#L596-L754)，以及 [`SessionThreadAgent`](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/agents/agents.ts#L830-L865)。

## `outcome_evaluations[]`

每个数组项的下列字段均为必有：

| 字段 | JSON 类型 | 说明 |
| --- | --- | --- |
| `completed_at` | `string \| null` | RFC 3339 完成时间。 |
| `description` | `string` | 期望 Agent 产出的描述。 |
| `explanation` | `string \| null` | 最近一次评估的解释。 |
| `iteration` | `number` | 从 0 开始的修订轮次。 |
| `outcome_id` | `string` | 服务端生成的 Outcome ID。 |
| `result` | `string` | 文档列举 `pending`、`running`、`evaluating`、`satisfied`、`max_iterations_reached`、`failed`、`interrupted`，但官方 SDK 当前仍声明为宽泛 `string`。 |
| `type` | `"outcome_evaluation"` | 固定判别字段。 |

依据：官方 SDK 的 [`BetaManagedAgentsOutcomeEvaluationResource`](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/sessions.ts#L584-L623)。

## `resources[]`

`resources` 是三种资源的联合类型：

### GitHub Repository Resource

| 字段 | JSON 类型 | 必有 |
| --- | --- | --- |
| `id` | `string` | 是 |
| `created_at` | `string`（RFC 3339） | 是 |
| `mount_path` | `string` | 是 |
| `type` | `"github_repository"` | 是 |
| `updated_at` | `string`（RFC 3339） | 是 |
| `url` | `string` | 是 |
| `checkout` | `{type:"branch",name:string} \| {type:"commit",sha:string} \| null` | 否 |

请求中的 `authorization_token` 不会在响应资源中回显。

### File Resource

| 字段 | JSON 类型 | 必有 |
| --- | --- | --- |
| `id` | `string` | 是 |
| `created_at` | `string`（RFC 3339） | 是 |
| `file_id` | `string` | 是 |
| `mount_path` | `string` | 是 |
| `type` | `"file"` | 是 |
| `updated_at` | `string`（RFC 3339） | 是 |

### Memory Store Resource

| 字段 | JSON 类型 | 必有 |
| --- | --- | --- |
| `memory_store_id` | `string` | 是 |
| `type` | `"memory_store"` | 是 |
| `access` | `"read_write" \| "read_only" \| null` | 否 |
| `description` | `string` | 否 |
| `instructions` | `string \| null` | 否 |
| `mount_path` | `string \| null` | 否 |
| `name` | `string \| null` | 否 |

依据：官方 SDK 的 [Session Resource 响应类型](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/resources.ts#L166-L264)。

## `stats` 与 `usage`

`stats` 根字段必有，但内部字段均可省略：

```text
{
  active_seconds?: number,
  duration_seconds?: number
}
```

`usage` 根字段必有，但内部字段均可省略：

```text
{
  cache_creation?: {
    ephemeral_1h_input_tokens?: number,
    ephemeral_5m_input_tokens?: number
  },
  cache_read_input_tokens?: number,
  input_tokens?: number,
  output_tokens?: number
}
```

依据：官方 SDK 的 [`BetaManagedAgentsSessionStats`](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/sessions.ts#L771-L786)、[`BetaManagedAgentsSessionUsage`](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/sessions.ts#L824-L847) 与 [`BetaManagedAgentsCacheCreationUsage`](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/sessions.ts#L397-L410)。

## 创建时的状态与事件回显

官方 Start a session 指南说明：

- 省略 `initial_events` 或传空数组时，只创建 Session 而不开始工作，Session 通常为 `idle`。
- 提供非空 `initial_events` 时，Session 直接以 `running` 状态创建。
- `initial_events` 会在返回创建响应前被逐条校验、持久化并分配事件 ID，但不会作为字段回显在 Create Session 响应中；如需查看，应调用事件列表接口。

依据：[Seed the session with initial events](https://platform.claude.com/docs/en/managed-agents/sessions#seed-the-session-with-initial-events)。

## 对兼容实现的核对清单

- [ ] 成功状态码为 200，而非 201。
- [ ] 返回完整 Session 对象，而非创建确认或局部对象。
- [ ] `agent` 返回解析后的完整快照。
- [ ] 根对象除 `deployment_id` 外的字段全部存在。
- [ ] `archived_at` 与 `title` 空值使用显式 `null`。
- [ ] 空集合字段返回 `[]`，`metadata` 返回对象。
- [ ] `stats`、`usage` 根对象始终存在。
- [ ] `status` 不返回 `pending` 等契约外枚举值。
- [ ] `agent.model.effort` 使用对象结构。
- [ ] GitHub Resource 不回显 `authorization_token`。
- [ ] `initial_events` 不在创建响应中回显。

## 来源说明

本次仅使用 Anthropic 第一方资料：

1. [Create Session API Reference](https://platform.claude.com/docs/en/api/beta/sessions/create)
2. [Start a session](https://platform.claude.com/docs/en/managed-agents/sessions)
3. [Anthropic 官方 TypeScript SDK API 索引](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/api.md#sessions)
4. [官方 SDK Sessions 源码与 OpenAPI 生成类型](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/sessions.ts)
5. [官方 SDK Session Resources 类型](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/sessions/resources.ts)
6. [官方 SDK Agent 嵌套类型](https://github.com/anthropics/anthropic-sdk-typescript/blob/3b45cd3b69c956ac63384fdb09ce1d8109f3fa80/src/resources/beta/agents/agents.ts)

官方 SDK 链接固定到核验时 `main` 分支提交 `3b45cd3b69c956ac63384fdb09ce1d8109f3fa80`，避免后续类型更新改变本次证据内容。

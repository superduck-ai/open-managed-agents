# Managed Agent 到 Claude Code 权限桥接设计

> 目标：在不修改 `environment-manager` 的前提下，把 Managed Agents 的 tool permission policy 映射到 Claude Code 运行时，让 MCP 和 agent toolset 的 `always_allow` / `always_ask` / `enabled=false` 语义在 Claude Code session 中正确生效。

---

## 1. 背景与结论

Managed Agents API 的权限模型定义在 agent snapshot 的 `tools` 中：

- `agent_toolset_20260401` 默认 `always_allow`。
- `mcp_toolset` 默认 `always_ask`。
- `default_config` 定义 toolset 默认行为。
- `configs[]` 定义单个工具的覆盖行为。

Claude Code 运行在 `environment-manager` 中，它通过 MCP config 加载 MCP server，并在工具执行前通过 `control_request / can_use_tool` 请求权限。二者不是同一个权限模型：

- Managed Agents 的 policy 是 API 产品契约，需要支持 default policy 和 per-tool override。
- Claude Code 的 CLI 参数，如 `--allowed-tools`、`--disallowed-tools`、`--permission-mode`，只能表达启动时的部分本地权限配置。
- `mcp_toolset.default_config` 对未来或未知 MCP tool 也生效，无法只靠启动时的 allow list 完整表达。

因此主方案是：`environment-manager` 继续只负责启动和透传；`claude-api-server` 在 Claude Code 发出 `can_use_tool` 时，按 session 的 `agent_snapshot.tools` 计算 effective policy，再向 Claude Code 发送 `control_response` 或向 Managed Agents client 暴露等待确认事件。

`--permission-mode bypassPermissions` 不作为 `always_allow` 的实现。它绕过范围太大，只适合本地排障或隔离环境中的临时诊断，不应承载 Managed Agents 的产品语义。

---

## 2. 三层映射

### 2.1 连接层

`managedAgentSessionConfig` 继续从 agent snapshot 中读取 `mcp_servers`，生成 Claude Code 可加载的 MCP config file：

```json
{
  "mcpServers": {
    "weather_service": {
      "type": "http",
      "url": "http://host.docker.internal:39090/mcp"
    }
  }
}
```

session config 继续通过现有字段传给 `environment-manager`：

```json
{
  "mcp_config_file": {
    "path": "/tmp/managed-agent-mcp-config.json",
    "content": "...base64...",
    "mode": 384
  },
  "claude_code_args": {
    "mcp-config": "/tmp/managed-agent-mcp-config.json"
  }
}
```

`environment-manager` 已经会把 `claude_code_args` 展开成 Claude Code CLI 参数，因此本设计不改变 v0 stdin schema，也不修改 `environment-manager` 代码。

### 2.2 静态提示层

显式 `mcp_toolset.configs[]` 可以继续写进 MCP config 的 `tools` 配置，作为 Claude Code 启动时可见的静态提示：

```json
{
  "name": "get_weather",
  "enabled": true,
  "permission_policy": "allow"
}
```

这层只能作为优化，不能作为最终权限裁决：

- `default_config.permission_policy` 适用于 server 下所有工具，包括 session 启动时尚未枚举的 MCP tool。
- Claude Code `--allowed-tools` 需要具体工具名，例如 `mcp__weather_service__get_weather`，无法完整表达“这个 MCP server 的所有当前和未来工具都按 default policy 处理”。
- 启动参数可能和运行时事件存在版本差异，最终行为必须以 server 端 agent snapshot 为准。

可选优化：对显式 `configs[]` 中的 allow/deny tool 生成 `allowed-tools` / `disallowed-tools`，减少 Claude Code prompt 次数。但即使做了该优化，runtime permission handler 仍是最终裁决者。

### 2.3 运行时权限层

Claude Code 执行工具前发出内部事件：

```json
{
  "type": "control_request",
  "request_id": "req_...",
  "request": {
    "subtype": "can_use_tool",
    "tool_name": "mcp__weather_service__get_weather",
    "tool_use_id": "tool_...",
    "input": {"location": "Beijing"}
  }
}
```

`claude-api-server` 必须在 worker batch endpoint 和单事件 endpoint 中统一处理该事件：

- 先持久化 outbound `control_request`，保证审计和去重。
- 调用统一 permission handler 计算 effective policy。
- 对 `allow` / `deny` 生成 inbound `control_response`。
- 对 `ask` 暴露 Managed Agents 的等待确认契约，等待客户端发送 `user.tool_confirmation`。

---

## 3. Effective Policy 模型

内部派生类型：

```ts
type ResolvedToolPermission = 'allow' | 'ask' | 'deny'

type ToolIdentity =
  | { kind: 'mcp'; serverName: string; toolName: string }
  | { kind: 'agent_toolset'; toolName: string }
  | { kind: 'unknown'; toolName: string }
```

### 3.1 Tool identity

MCP tool 按 Claude Code 工具名解析：

```text
mcp__<server>__<tool>
```

例如：

```text
mcp__weather_service__get_weather
=> serverName = weather_service
=> toolName = get_weather
```

agent toolset 的工具名需要归一化到 Managed Agents 配置使用的名字。建议建立显式映射，避免大小写或 Claude Code 内部命名差异造成误判：

| Managed Agent name | Claude Code tool name examples |
|---|---|
| `bash` | `Bash` |
| `edit` | `Edit`, `MultiEdit` |
| `read` | `Read` |
| `write` | `Write` |
| `glob` | `Glob` |
| `grep` | `Grep` |
| `web_fetch` | `WebFetch` |
| `web_search` | `WebSearch` |

无法识别的工具按 `unknown` 处理。`unknown` 不应被默认放行；除非后续有明确产品决策，默认按 `ask` 或 deny-safe 策略处理。

### 3.2 Policy 解析优先级

MCP tool：

1. 解析 `mcp__<server>__<tool>`。
2. 在 agent snapshot 的 `tools[]` 中找到 `type=mcp_toolset` 且 `mcp_server_name=<server>` 的 toolset。
3. 如果存在 `configs[]` 且 `name=<tool>`，使用该 config。
4. 否则使用该 toolset 的 `default_config`。
5. 如果缺少 toolset，按 MCP 默认 `always_ask` 兼容旧 snapshot。

agent toolset：

1. 归一化 Claude Code tool name 到 Managed Agent tool name。
2. 找到 `type=agent_toolset_20260401` 的 toolset。
3. 如果存在 `configs[]` 且 `name=<tool>`，使用该 config。
4. 否则使用 `default_config`。
5. 如果缺少 agent toolset，按默认 `always_allow` 兼容既有 agent。

最终映射：

| Managed Agent effective config | Runtime permission |
|---|---|
| `enabled=false` | `deny` |
| `permission_policy.type=always_allow` | `allow` |
| `permission_policy.type=always_ask` | `ask` |
| 缺失或无法解析 | `ask`，并记录诊断日志 |

`enabled=false` 优先级高于 permission policy。也就是说，即使 policy 是 `always_allow`，只要 tool 被禁用，运行时也必须 deny。

---

## 4. 事件桥接

### 4.1 `always_allow`

当 permission handler 解析为 `allow`：

1. 不把内部 `control_request` 投影到 public session events。
2. 生成 inbound `control_response`：

```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "req_...",
    "response": {
      "behavior": "allow",
      "toolUseID": "tool_...",
      "updatedInput": {"location": "Beijing"}
    }
  }
}
```

3. inbound event source 使用 `auto-approve`。
4. duplicate worker event 不重复写入 control response。

### 4.2 `enabled=false`

当 permission handler 解析为 `deny`：

1. 不把内部 `control_request` 投影到 public session events。
2. 生成 inbound `control_response`，`behavior` 为 `deny`。
3. 如果 Claude Code control protocol 需要 message 字段，使用稳定、非敏感文案，例如 `Tool is disabled by the agent permission policy.`
4. 保留 outbound `control_request` 和 inbound `control_response` 供内部审计。

### 4.3 `always_ask`

当 permission handler 解析为 `ask`：

1. 不自动发送 Claude Code `control_response`。
2. 将该阻塞请求投影成 Managed Agents public event 契约：
   - `agent.tool_use` 或 `agent.mcp_tool_use` 带 `evaluated_permission=ask`。
   - session 进入 idle / requires_action 状态。
   - `stop_reason.event_ids` 包含阻塞 tool use public event 的 `id`，例如 `agent.tool_use.id` 或 `agent.mcp_tool_use.id`（`sevt_...`），不是 Claude Code worker `request.tool_use_id`。
   - `stop_reason` 遵循 SDK union shape，只包含官方字段 `type` / `event_ids`；`tool_use_id`、`tool_name`、`request_id`、`session_thread_id` 等兼容诊断字段保留在同事件的 `requires_action_details`。
3. 等待 API client 使用 `stop_reason.event_ids` 中的 public event id 发送：

```json
{
  "type": "user.tool_confirmation",
  "tool_use_id": "sevt_...",
  "result": "allow"
}
```

4. 收到 confirmation 后，server 将 public event id 解析到对应阻塞 `agent.tool_use` / `agent.mcp_tool_use` 事件，再读取 payload 中的 worker `tool_use_id` 查找 pending `can_use_tool` 请求，生成 Claude Code inbound `control_response`：
   - `result=allow` -> `behavior=allow`
   - `result=deny` -> `behavior=deny`，携带 `deny_message`
5. 为兼容已有客户端，`user.tool_confirmation.tool_use_id` 仍接受旧 worker `request.tool_use_id`（`tool_...`）并按旧路径直接查找 pending 请求。
6. 子线程工具确认使用相同语义：cross-post 到 primary 的阻塞 public event 带 `session_thread_id`，`stop_reason.event_ids` 指向该 public event id；确认时即使只传 public event id，server 也应把 `session_thread_id` 保留到 worker `control_response`。
7. `user.tool_confirmation` 本身保留为 public session event，用于审计和 Session Detail 只读回放。

### 4.4 Batch 与单事件一致性

Claude Code 可能通过 `/worker/events` batch endpoint 上报 `can_use_tool`。因此 permission handler 不能只挂在单事件 `appendWorkerEvent` 路径上。

要求：

- `AppendWorkerOutputEventsForEpoch` 和 `appendWorkerEvent` 处理 `control_request / can_use_tool` 时调用同一逻辑。
- 对 duplicate outbound event 必须保持幂等，不重复发送 auto response。
- `control_request`、`control_response`、`control_cancel_request` 仍是内部协议事件，默认不泄漏到 public session events。

---

## 5. 与 Claude Code CLI 参数的关系

本设计保留 `claude_code_args["mcp-config"]` 作为必需启动参数。其他 Claude Code 权限参数只作为可选优化或诊断手段。

| Claude Code 参数 | 设计定位 |
|---|---|
| `--mcp-config` | 必需，用于加载 Managed Agent MCP servers。 |
| `--allowed-tools` | 可选优化，只适合显式已知 allow tools。 |
| `--disallowed-tools` | 可选优化，只适合显式已知 disabled tools。 |
| `--permission-mode dontAsk` | 不用于 Managed Agents 默认实现，会把 ask 变成自动 deny，不符合确认事件契约。 |
| `--permission-mode bypassPermissions` | 仅限本地排障，不作为 `always_allow` 的产品实现。 |

原因：

- Managed Agents 的 `always_ask` 需要公开等待确认事件，而不是让 Claude Code 自己在容器内交互式询问。
- Managed Agents 的 `default_config` 是 server 级产品语义，必须由 server 端按 agent snapshot 决定。
- CLI 参数是启动配置，无法可靠替代运行时事件桥接、审计和 API confirmation 流程。

---

## 6. 实现边界

不新增公开 API，不修改 `environment-manager`。

实现应集中在 `claude-api-server`：

- Managed Agent session config 继续生成 MCP config file 和 `claude_code_args["mcp-config"]`。
- Code session service 新增 policy-aware permission handler。
- Session events 接收 `user.tool_confirmation` 后，将 public blocking event id 解析为 worker `tool_use_id`，补齐到 Claude Code `control_response` 的路由；旧 worker `tool_use_id` 作为兼容输入继续支持。
- 日志只记录 tool name、server name、resolved permission、code session id、request id 等诊断字段；不要记录 secret、header value 或完整 tool input。

非目标：

- 不把所有 session 改成 `bypassPermissions`。
- 不改变 public Managed Agents API 字段。
- 不要求 `environment-manager` 理解 Managed Agent permission policy。
- 不要求启动前枚举 MCP server 的全部 tools。

---

## 7. 测试与验收

### 7.1 Effective policy 单元测试

覆盖：

- MCP `configs[]` 覆盖 `default_config`。
- MCP `default_config=always_allow` 且 `configs=[]` 自动 allow。
- MCP 无 toolset 或旧 snapshot 默认 ask。
- MCP `enabled=false` 自动 deny。
- agent toolset 默认 allow。
- agent toolset 单工具 config 可覆盖为 ask 或 deny。
- 无法解析的 tool name 默认 ask，并产生诊断日志。

### 7.2 Code session 测试

覆盖：

- batch `/worker/events` 中的 `can_use_tool` 会进入 permission handler。
- 单事件 worker append 路径和 batch 路径行为一致。
- worker `result.stop_reason` 为字符串时，public `session.status_idle.stop_reason` 会规范化为 SDK 对象 union，例如 `{ "type": "end_turn" }`。
- `always_allow` 生成 inbound `control_response`，source 为 `auto-approve`。
- duplicate worker event 不重复生成 auto response。
- `always_ask` 不 auto approve。
- `enabled=false` 生成 deny response。
- internal `control_request` 不出现在 public session events。
- `always_ask` 生成的 `session.status_idle.stop_reason` 遵循 SDK union shape，`event_ids` 包含阻塞 `agent.tool_use` / `agent.mcp_tool_use` 的 public event id，兼容诊断字段只出现在 `requires_action_details`。

### 7.3 Confirmation 测试

覆盖：

- `always_ask` tool call 暂停后，用 `stop_reason.event_ids` 中的 public event id 发送 `user.tool_confirmation(result=allow)` 会生成 Claude Code allow response。
- 用 public event id 发送 `user.tool_confirmation(result=deny, deny_message=...)` 会生成 Claude Code deny response。
- 继续用旧 worker `request.tool_use_id` 发送 confirmation 仍会生成对应 Claude Code response。
- confirmation 找不到 pending tool use 时返回 Anthropic-compatible validation error。
- subagent cross-post 的 permission request 能通过 public blocking event id 路由回原 code session，并在 worker response 中保留 `session_thread_id`。

### 7.4 集成验收

使用以下 agent 配置新建 session：

```yaml
name: MyMcpTest
model:
  id: claude-sonnet-4-6
mcp_servers:
  - name: weather_service
    type: url
    url: http://host.docker.internal:39090/mcp
tools:
  - type: agent_toolset_20260401
    default_config:
      enabled: true
      permission_policy:
        type: always_allow
  - type: mcp_toolset
    mcp_server_name: weather_service
    configs: []
    default_config:
      enabled: true
      permission_policy:
        type: always_allow
```

期望：

- 新 session 的 agent snapshot 包含 `weather_service` 和 `mcp_toolset`。
- `/tmp/managed-agent-mcp-config.json` 包含 `weather_service`。
- Claude Code init event 显示 MCP server connected。
- 调用 `mcp__weather_service__get_weather` 时不再卡在 permission prompt。
- DB 中能看到 `can_use_tool` outbound 和对应 auto `control_response` inbound。

再将 `mcp_toolset.default_config.permission_policy` 改为 `always_ask` 后新建 session，期望：

- session 暂停并公开等待确认。
- `session.status_idle.stop_reason.event_ids` 指向阻塞 tool use 的 public event id。
- `session.status_idle.stop_reason` 不包含 `tool_use_id`、`tool_name`、`request_id`、`session_thread_id` 等非 SDK 字段。
- 客户端用该 public event id 发送 `user.tool_confirmation` 后，Claude Code 继续执行。

---

## 8. 兼容性说明

已创建的 session/code session 使用创建时的 agent snapshot，不会自动跟随 agent 最新配置变化。验证权限配置修改时必须新建 session。

旧 snapshot 中如果存在 `mcp_servers` 但缺少对应 `mcp_toolset`，按 MCP 默认 `always_ask` 处理，避免无意放行。

为兼容早期实现，`user.tool_confirmation.tool_use_id` 仍可传 Claude Code worker `request.tool_use_id`；官方契约和新客户端应使用 `session.status_idle.stop_reason.event_ids` 中的 public event id。

如果后续 Claude Code MCP config 正式支持 server-level default permission policy，可以把静态提示层扩展为写入该字段；runtime permission handler 仍应保留为最终裁决和审计路径。

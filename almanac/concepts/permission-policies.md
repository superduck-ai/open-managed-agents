---
title: "Permission Policies"
summary: "Permission policies control when agent tools and MCP tools execute automatically versus requiring user approval, configured through toolset defaults and per-tool overrides."
topics: [permissions, tools, mcp, security]
sources:
  - id: permission-policies
    type: file
    path: docs/design/be/permission-policies.md
  - id: permission-bridge
    type: file
    path: docs/design/be/managed-agent-claude-code-permission-bridge.md
---

Permission policies govern the execution timing of server-side tools in managed agents. They determine whether tools run automatically or wait for user approval, providing a security and control boundary between autonomous agent behavior and human oversight. Policies apply to the built-in agent toolset (Bash, Edit, Read, Write, etc.) and MCP server tools, but not to custom tools which are executed by the client application.

## Policy Types

The system supports two policy types [@permission-policies]:

- `always_allow`: Tools execute immediately without confirmation
- `always_ask`: Sessions pause and wait for user approval before execution

Each toolset type has a default policy. The agent toolset defaults to `always_allow` for hands-off interaction, while MCP toolsets default to `always_ask` since external MCP servers may expose new or unknown tools [@permission-policies].

## Configuration Structure

Policies are configured within an agent's `tools` array. Each toolset entry specifies a `type` (`agent_toolset_20260401` or `mcp_toolset`), a `default_config` with the default policy, and optional `configs` array for per-tool overrides [@permission-policies]. For MCP toolsets, the `mcp_server_name` must match a server defined in the agent's `mcp_servers` array.

The default config applies to all tools in the toolset unless overridden. Individual tool entries in `configs` specify a `name` matching the tool identifier and a `permission_policy` that replaces the default for that specific tool. Additionally, tools can be completely disabled via `enabled=false`, which takes precedence over the permission policy [@permission-bridge].

## Policy Resolution

At runtime, when Claude Code requests tool execution through a `can_use_tool` control event, the permission handler resolves the effective policy by [@permission-bridge]:

1. Parsing the tool name to extract server and tool identifiers (e.g., `mcp__weather_service__get_weather` → server=`weather_service`, tool=`get_weather`)
2. Locating the matching toolset in the agent snapshot by type and server name
3. Checking for a per-tool config matching the tool name
4. Falling back to the toolset's `default_config` if no per-tool config exists
5. Applying default fallbacks (`always_ask` for MCP without toolset, `always_allow` for agent tools) for legacy snapshots

The resolved policy maps to a runtime permission: `enabled=false` becomes `deny`, `permission_policy.type=always_allow` becomes `allow`, and `permission_policy.type=always_ask` becomes `ask` [@permission-bridge].

## Always Ask Flow

When a tool resolves to `ask`, the session enters an approval flow [@permission-policies]:

1. The tool use is projected as a public `agent.tool_use` or `agent.mcp_tool_use` event with `evaluated_permission=ask`
2. The session transitions to `idle` status with `stop_reason.type=requires_action`
3. The `stop_reason.event_ids` array contains the public event ID of the blocking tool use
4. The session waits indefinitely for a `user.tool_confirmation` event

The client sends a confirmation event with the tool use event ID from `stop_reason.event_ids`, setting `result` to either `allow` or `deny` (optionally with a `deny_message`) [@permission-policies]. Upon receiving the confirmation:

- For `allow`: The runtime sends a Claude Code `control_response` with `behavior=allow`, and the tool executes
- For `deny`: The runtime sends a `control_response` with `behavior=deny` and the deny message, and the agent receives a tool result indicating the call was rejected

Multiple tools can be blocked simultaneously, and the client can send confirmations for all of them in a single `events` request. The session returns to `running` status once all blocking events are resolved.

## Permission Bridge Implementation

The permission bridge lives in the code session runtime's tool permission handler. It processes `control_request/can_use_tool` events from Claude Code, evaluates the agent snapshot's tool configuration, and responds via `control_response` messages or public session events depending on the resolved policy [@permission-bridge].

The bridge maintains a mapping from public event IDs to pending worker request IDs, allowing `user.tool_confirmation` events addressed to public events to route back to the correct Claude Code control response. For backward compatibility, it also accepts confirmation by the original worker `tool_use_id` [@permission-bridge].

Internal control events (`control_request`, `control_response`, `control_cancel_request`) are not projected to the public session event stream. Only the final tool use outcomes and any blocking situations become visible to API clients, keeping the permission protocol private while exposing the approval surface through standardized events.

## Safety and Defaults

The conservative default for MCP tools (`always_ask`) ensures that newly added tools from external servers don't execute without review. Unknown or unparseable tool names default to `ask` with diagnostic logging, preventing accidental elevation of unrecognized tools [@permission-bridge].

The `enabled=false` disable flag operates independently of permission policies, providing a hard-off switch for tools that shouldn't be available regardless of approval flow. This supports scenarios where certain capabilities (like unrestricted bash execution) need to be completely removed from an agent's toolkit.

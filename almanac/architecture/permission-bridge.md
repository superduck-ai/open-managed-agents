---
title: "Permission Bridge"
summary: "The permission bridge translates managed agent tool permission policies into Claude Code runtime control responses and public waiting confirmation events."
topics: [architecture]
sources:
  - id: permission-bridge-design
    type: file
    path: docs/design/be/managed-agent-claude-code-permission-bridge.md
  - id: tool-permissions
    type: file
    path: internal/codesessions/tool_permissions.go
  - id: code-service
    type: file
    path: internal/codesessions/service.go
  - id: env-manager
    type: file
    path: internal/environments/environment_manager.go
---

The permission bridge connects the managed agents permission model with Claude Code's runtime permission protocol. When Claude Code requests permission to use a tool, the bridge evaluates the agent snapshot's tool policies and responds with allow, deny, or ask actions—converting "ask" policies into public waiting confirmation events that API clients can resolve.

## Permission Resolution

When Claude Code emits a `control_request` event with `subtype: "can_use_tool"`, the code session service resolves the effective permission by parsing the tool name and consulting the agent snapshot's tools configuration [@tool-permissions].

Tool names are parsed by kind:
- **MCP tools**: Format `mcp__<server>__<tool>`, extracting server name and tool name
- **Agent tools**: Recognized by known Claude Code tool names (`Bash`, `Edit`, `Read`, etc.) and mapped to managed agent tool names (`bash`, `edit`, `read`, etc.)
- **Unknown tools**: Tools that don't match known patterns

Resolution follows a priority order:
1. Check for explicit tool config in the toolset's `configs[]` array
2. Fall back to the toolset's `default_config`
3. Use default behavior if no toolset exists (MCP defaults to `always_ask`, agent tools default to `always_allow`)

The `enabled=false` setting overrides any permission policy—disabled tools are always denied regardless of their policy type [@permission-bridge-design].

## Effective Policy Mapping

The bridge maps managed agent permission policies to runtime actions:

| Managed Agent Config | Runtime Permission | Behavior |
|---------------------|-------------------|----------|
| `enabled=false` | deny | Generate auto-deny response |
| `permission_policy: always_allow` | allow | Generate auto-approve response |
| `permission_policy: always_ask` | ask | Publish waiting confirmation event |
| Missing config | ask (MCP) / allow (agent) | Use default fallback |

For `allow` and `deny` cases, the bridge generates an inbound `control_response` event with the corresponding behavior and pushes it to the worker. These responses are marked with source `auto-approve` or `auto-deny` for audit trails [@tool-permissions].

## Ask Flow and Public Events

When a tool permission resolves to `ask`, the bridge does not immediately respond to Claude Code. Instead, it publishes public events that transition the session to a waiting state:

1. Generate a public `agent.tool_use` or `agent.mcp_tool_use` event with `evaluated_permission: "ask"`
2. Generate a `session.status_idle` event with `stop_reason.type: "requires_action"` and `event_ids` pointing to the blocking tool use event
3. The session stops processing and waits for a `user.tool_confirmation` event

The `stop_reason.event_ids` array contains the public event ID that clients must reference when sending confirmations. For backward compatibility, the bridge also accepts the worker's internal `tool_use_id` in confirmation requests [@permission-bridge-design].

## Tool Confirmation

When an API client sends a `user.tool_confirmation` event with `tool_use_id` and `result`, the code session service routes the confirmation to the pending permission request [@tool-permissions]. The confirmation flow:

1. Parse the `tool_use_id` as either a public event ID or legacy worker tool use ID
2. Look up the pending `can_use_tool` request by tool use ID or via the public event's payload
3. Generate an inbound `control_response` with behavior derived from `result` (`allow` or `deny`)
4. Push the response to the worker, allowing Claude Code to continue

For `deny` confirmations, clients can optionally provide a `deny_message` that surfaces in the Claude Code UI.

## Subagent Thread Routing

When subagents request tool permissions, the bridge maintains `session_thread_id` context through the permission flow. The blocking public event includes the session thread ID, and confirmations are routed back to the correct worker session by matching this thread ID [@permission-bridge-design].

This allows a subagent running in a separate thread to request permission for a tool use, have the request published as a public event in the primary session's event stream, and have the confirmation routed back to the correct subagent worker context.

## Environment Manager Integration

The environment manager configuration passes `DangerouslySkipPermissions: true` and `PermissionMode: "bypassPermissions"` to the code session service [@env-manager]. This setting disables Claude Code's built-in permission prompts since the permission bridge handles all permission logic at the server level.

The bridge operates as the sole permission authority, ensuring that Claude Code never makes independent permission decisions and that all tool usage is governed by the agent snapshot's declared policies.

## Batch and Single Event Consistency

Permission requests arrive via both the single `AppendWorkerEvent` endpoint and the batch `AppendWorkerOutputEventsForEpoch` endpoint. Both paths call the same `handleToolPermissionRequest` logic, ensuring consistent behavior regardless of how Claude Code batches its control requests [@code-service].

The bridge also handles duplicate events gracefully—if the same `can_use_tool` request is processed multiple times (due to retries or idempotency), it only generates a single response per request ID.

---
title: "Claude Code Runtime"
summary: "The Claude Code runtime integrates Claude Code's code execution capabilities with managed agent sessions through event streaming, permission bridging, and environment lifecycle management."
topics: [claude-code-runtime, code-sessions, environments]
sources:
  - id: readme-cn
    type: file
    path: README.cn.md
  - id: codesessions-service
    type: file
    path: internal/codesessions/service.go
  - id: codesessions-ingress
    type: file
    path: internal/codesessions/ingress.go
  - id: permission-bridge
    type: file
    path: docs/design/be/managed-agent-claude-code-permission-bridge.md
  - id: db-schema
    type: file
    path: internal/db/migrations/00001_init.sql
---

The Claude Code runtime bridges managed agent sessions with Claude Code's code execution sandbox capabilities. It enables agents to read, write, and execute code in isolated environments while maintaining permission control and event streaming compatibility with the Managed Agents API.

## Code Sessions

When a managed agent session requires code execution, the system creates a `code_session` that links the session to an environment sandbox [@db-schema]. The code session tracks the connection status (`connected`/`disconnected`), permission mode, model configuration, and work directory for the Claude Code instance [@db-schema].

Code sessions communicate through bidirectional event queues. `code_session_inbound_events` deliver control requests and public session events from the API to the worker, while `code_session_outbound_events` stream worker responses, tool use requests, and execution results back to the API layer [@db-schema]. Both queues use sequence numbers and idempotency keys for reliable, exactly-once delivery.

## Event Ingress and Protocol

The runtime exposes WebSocket and HTTP endpoints for worker connectivity. Workers connect to `/v1/code/sessions/{code_session_id}/worker` and receive inbound events as newline-delimited JSON [@codesessions-ingress]. The protocol distinguishes between control events (internal protocol messages like `can_use_tool` requests) and public events that project into the managed agent session history.

Worker events are normalized into `control_request`, `control_response`, and `control_cancel_request` subtypes for permission handling, plus standard output events like `request.tool_use` and `response.text_delta` [@codesessions-service]. The runtime calculates content hashes and validates sequence ordering to detect duplicates or out-of-order delivery.

## Permission Bridging

A key responsibility of the runtime is translating managed agent permission policies into Claude Code's permission model. The agent's `tools` configuration specifies whether tools should `always_allow`, `always_ask`, or be disabled via `enabled=false` [@permission-bridge]. When Claude Code requests tool use through a `control_request/can_use_tool` event, the runtime evaluates the effective policy against the agent snapshot and responds accordingly:

- For `always_allow` tools, the runtime immediately sends a `control_response` with `behavior=allow` without exposing the request to the public session
- For disabled tools, it responds with `behavior=deny` 
- For `always_ask` tools, it projects the request as a public `agent.tool_use` event with `evaluated_permission=ask`, causing the session to idle and wait for a `user.tool_confirmation` event from the client [@permission-bridge]

This bridging happens in the `handleToolPermissionRequest` path, which maps Claude Code tool names like `mcp__weather_service__get_weather` back to MCP server and tool identifiers for policy resolution [@permission-bridge].

## Public Event Projection

The runtime filters and transforms worker events before they reach the managed agent session. Internal control events remain private, while output events like `request.tool_use`, `response.text_delta`, and `response.stop` are projected as public session events with stable event IDs derived from the code session ID and worker sequence number [@codesessions-service].

Subagent internal events require special handling—the runtime maps thread creation events to discover subagent-to-thread relationships, then projects subagent outputs into the appropriate thread context using the derived thread ID [@codesessions-service].

## Environment Lifecycle

Code sessions depend on `environment_work` records that the environment runner polls to start sandboxes [@readme-cn]. The work record specifies the environment template, runtime configuration, and metadata including the managed agent skill mounts. Sandboxes transition through `queued`, `starting`, `active`, `stopping`, and `stopped` states with heartbeat tracking for health monitoring [@db-schema].

The runtime doesn't directly manage sandboxes—instead, it prepares work records and delegates actual sandbox lifecycle to the environment runner, which claims work, starts containers, and streams results back through the code session event bridge.

## Worker Registration and Recovery

Active workers register themselves with the code session service via WebSocket connections. The service maintains a map of active workers by code session ID and pushes inbound events to connected workers in real-time [@codesessions-service]. When workers disconnect or reconnect, the sequence-based event delivery ensures they can resume from their last processed position without duplicating work.

For disconnected workers, inbound events queue in the database with `delivery_status=queued` until the worker reconnects and requests the event batch. This design allows workers to be ephemeral while maintaining reliable delivery.

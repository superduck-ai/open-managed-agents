---
title: "Debugging Sessions"
summary: "Techniques for diagnosing and troubleshooting session execution issues in Open Managed Agents."
topics: [debugging, sessions, troubleshooting]
sources:
  - id: sessions-service
    type: file
    path: internal/sessions/service.go
  - id: codesessions-service
    type: file
    path: internal/codesessions/service.go
  - id: permission-policies
    type: file
    path: docs/design/be/permission-policies.md
  - id: worker-events-delivery
    type: file
    path: docs/design/be/ccrv2/ccr-v2-worker-events-delivery-backend-design.md
  - id: worker-heartbeat
    type: file
    path: docs/design/be/ccrv2/worker-heartbeat-server-implementation.md
  - id: worker-state
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-state.md
  - id: sessions-handler
    type: file
    path: internal/sessions/handler.go
---

Debugging sessions in Open Managed Agents requires understanding the interaction between public sessions, code sessions, worker events, and the event delivery system. This guide covers debugging techniques and common troubleshooting scenarios.

## Session Architecture Overview

Sessions operate through a dual-layer architecture:

1. **Public Sessions** (`/v1/sessions`) - The API surface that clients interact with
2. **Code Sessions** (`/v1/code/sessions`) - The worker execution layer that manages Claude Code runtime

Event flow between these layers follows specific patterns. User messages sent to the public session are queued as inbound events for the code session [@codesessions-service]. Worker outputs are published back to the public session through the event mapper [@sessions-service].

## Common Debugging Scenarios

### Session Not Receiving Worker Events

When a session appears stuck because worker events aren't appearing in the public event stream, check the following:

**Verify code session creation**: The code session must be successfully created and linked to the public session. Check that the session's `code_session_id` is properly set in the database.

**Check worker registration**: Workers must register via `/worker/register` to establish ownership. An unregistered worker cannot publish events. Verify `current_worker_epoch > 0` in the code session record.

**Confirm event delivery status**: Events flow through `code_session_inbound_events` with delivery status tracking. Check `delivery_status` values:
- `queued` - Event stored but not sent to worker
- `sent` - SSE frame written but no worker ACK
- `received` - Worker acknowledged receipt
- `processing` - Worker is processing
- `processed` - Worker completed handling

### Debugging Event Streams

The session event stream (`/v1/sessions/{id}/events/stream`) forwards canonical events from worker output. When debugging stream issues:

**Check event filtering**: The stream only forwards events where `maevents.IsPublicSessionHistoryEvent(eventType)` returns true. Internal control events like `control_request` are hidden [@sessions-handler].

**Verify thread mapping**: Multi-agent sessions create child threads. Events from subagents are backfilled only when the thread is first accessed. Use `backfillSubagentThreadEventsIfEmpty` to populate child thread events on first read [@sessions-handler].

**Inspect payload transformation**: Worker payloads undergo normalization through `BuildEventMetadata`. The `payload_uuid` must match between SSE envelopes and delivery ACKs for proper event correlation [@codesessions-service].

### MCP Tool Permission Issues

When MCP tools aren't executing as expected:

**Check permission policy**: MCP toolsets default to `always_ask`. Verify the agent's `mcp_toolset` configuration has the correct `default_config.permission_policy` [@permission-policies].

**Trace tool confirmation flow**: Tools with `always_ask` policy generate:
1. `agent.mcp_tool_use` event with `evaluated_permission: "ask"`
2. `session.status_idle` event with `stop_reason.type: "requires_action"`
3. Blocking event IDs in `stop_reason.event_ids`

The session waits indefinitely for `user.tool_confirmation` events matching those event IDs [@permission-policies].

**Verify auto-approve logic**: Tools with `always_allow` policy generate `control_response` events automatically through the `handleToolPermissionRequest` path without pausing the session [@codesessions-service].

## Testing Utilities

The test suite provides helpers for debugging session behavior:

**Session lifecycle tests** (`tests/sessions_api_test.go`) demonstrate:
- Creating sessions with resources (files, memory stores)
- Sending events and verifying persistence
- Listing sessions, threads, and events with pagination
- Archiving and deleting sessions

**Code session tests** cover:
- Worker registration and epoch management
- Event ingress from worker output
- Internal event persistence and replay
- Delivery ACK handling and state progression

**Stream tests** validate:
- SSE event delivery to clients
- Event delta streaming without persistence
- WebSocket fallback for code session transport

## Database Inspection

Key database tables for session debugging:

**`sessions`** - Public session records with status, metadata, and resource references
**`session_threads`** - Thread records including primary and child threads for multi-agent sessions
**`session_events`** - Canonical public events with thread association
**`code_sessions`** - Worker session records including epoch, lease, and connection status
**`code_session_inbound_events`** - Events queued for worker delivery with ACK tracking
**`code_session_outbound_events`** - Events published by worker for public session projection

## Worker Connection Debugging

Worker connectivity issues manifest as sessions stuck in intermediate states:

**Verify heartbeat status**: Check `worker_lease_expires_at` and `worker_last_heartbeat_at`. If `now > worker_lease_expires_at + grace_period`, the worker lease has expired [@worker-heartbeat].

**Check epoch conflicts**: Requests with mismatched `worker_epoch` return `409 conflict_error`. Old workers sending events after a new worker registers are rejected [@worker-state].

**Inspect connection state**: `connection_status` should be `connected` for active workers. A status of `disconnected` with a recent `last_worker_connected_at` suggests a connection drop.

## Event Replay and Recovery

The event delivery system supports worker restart and event replay:

**Unprocessed events persist**: Events with `delivery_status < processed` remain available for replay. New worker epochs can replay these events from the appropriate sequence number [@worker-events-delivery].

**Sequence-based cursor**: Workers provide `from_sequence_num` or `Last-Event-ID` to resume from their last acknowledged position, preventing duplicate event processing.

**Idempotency through payload hash**: Events with matching `payload_uuid` and `payload_hash` are deduplicated, preventing replay from creating duplicate public events.

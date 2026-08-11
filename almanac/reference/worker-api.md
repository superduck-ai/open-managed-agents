---
title: "Worker API"
summary: "The Worker API provides endpoints for Claude Code workers to manage state, receive events via SSE, send events, and maintain ownership through epoch-based leases."
topics: [reference, api, code-sessions]
sources:
  - id: worker-state-design
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-state.md
  - id: codesessions-ingress
    type: file
    path: internal/codesessions/ingress.go
  - id: db-code-sessions
    type: file
    path: internal/db/code_sessions.go
  - id: worker-events-delivery
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-events-delivery.md
---

# Worker API

The Worker API is a set of HTTP endpoints that Claude Code workers use to communicate with the managed agents backend. These endpoints handle worker registration, state persistence, event streaming, delivery acknowledgments, and liveness monitoring. The API is designed around epoch-based ownership to prevent multiple workers from interfering with each other[@worker-state-design].

## Worker Registration

`POST /v1/code/sessions/{session_id}/worker/register`

A new worker must register before it can interact with a code session. Registration increments the `current_worker_epoch` counter and establishes this worker as the owner[@worker-state-design][@db-code-sessions].

**Request Body**: Optional JSON with a `session_id` field for validation

**Response**:
```json
{
  "worker_epoch": "1"
}
```

The first registration returns epoch 1, and each subsequent registration increments the epoch. Workers from previous epochs continue to receive 409 (conflict) errors on write operations. The registration also updates lease expiration timestamps and worker binding metadata[@worker-state-design].

## Worker State Management

`PUT /v1/code/sessions/{session_id}/worker`

Workers persist lightweight state and metadata via the PUT endpoint[@worker-state-design][@db-code-sessions].

**Request**:
```json
{
  "worker_epoch": 1,
  "worker_status": "requires_action",
  "requires_action_details": {
    "tool_name": "Bash",
    "action_description": "Running npm test",
    "request_id": "req_..."
  },
  "external_metadata": {
    "pending_action": {"tool_name": "Bash"},
    "task_summary": null
  }
}
```

**Field Rules**:
- `worker_epoch`: Required positive integer (0 is invalid)
- `worker_status`: Must be `idle`, `running`, or `requires_action`
- `requires_action_details`: Object or null (cleared when status != requires_action)
- `external_metadata`: Object applied as a one-level merge patch

**Epoch Validation**: The endpoint checks that the request's `worker_epoch` matches the persisted `current_worker_epoch`. A mismatch returns 409 conflict error[@worker-state-design].

**Side Effects**: A successful PUT updates connection status, activity timestamps, and synchronizes the public session status to match the worker status (running → running, idle/requires_action → idle).

`GET /v1/code/sessions/{session_id}/worker`

Workers retrieve their persisted state via GET[@worker-state-design].

**Response**:
```json
{
  "worker": {
    "external_metadata": {
      "pending_action": {"tool_name": "Bash"},
      "task_summary": "Running tests"
    }
  }
}
```

The GET response includes only the `external_metadata` field in a minimal shape. An optional `worker_epoch` query parameter can validate ownership without updating state.

## Event Streaming

`GET /v1/code/sessions/{session_id}/worker/events/stream`

Workers receive inbound events (user messages, control requests) via Server-Sent Events[@worker-events-delivery]. The SSE stream pushes events with sequence numbers and delivery status tracking.

**Query Parameters**:
- `worker_epoch`: Optional epoch for ownership validation
- `from_sequence_num`: Starting sequence number for replay (defaults to 0)

**SSE Format**: Each event includes `data` (the payload), `id` (sequence number), and `event` type. Workers use `Last-Event-ID` header to resume after disconnection.

**Replay Behavior**: When a worker reconnects, the stream replays all events that haven't reached `processed` delivery status, respecting epoch boundaries. Events from previous worker epochs are not replayed to the new worker.

## Event Output

`POST /v1/code/sessions/{session_id}/worker/events`

Workers send outbound events (assistant messages, tool calls, status changes) via the events endpoint[@codesessions-ingress]. Each event requires a valid `worker_epoch`.

**Request**:
```json
{
  "worker_epoch": 1,
  "events": [
    {
      "type": "assistant",
      "content": [{"type": "text", "text": "..."}]
    }
  ]
}
```

Events are queued for webhook delivery and public SSE streaming. The endpoint validates epoch and prevents stale workers from sending events.

## Internal Events

`POST /v1/code/sessions/{session_id}/worker/internal-events`

Internal worker events (tool permission requests, compaction markers, etc.) use a separate endpoint[@codesessions-ingress][@db-code-sessions]. These events are not exposed in the public session history but are persisted for worker coordination.

**Behavior**: Similar to public events but with `internal` source flag and optional is_compaction marker for context compaction events.

## Delivery Acknowledgments

`POST /v1/code/sessions/{session_id}/worker/events/delivery`

Workers acknowledge receipt and processing of inbound events via delivery ACKs[@worker-events-delivery].

**Request**:
```json
{
  "worker_epoch": 1,
  "updates": [
    {
      "event_id": "5e34a4de-456f-4bb5-ba2c-152cf71d3fa1",
      "status": "processing"
    }
  ]
}
```

**Status Machine**: Events progress through `received` → `processing` → `processed`. Workers report `received` when SSE delivers an event, `processing` when command execution starts, and `processed` when command completes.

**Batching**: The endpoint accepts up to 64 updates per request. Unknown or stale event IDs are ignored and counted in the response's `ignored` field rather than causing errors.

## Heartbeat

`POST /v1/code/sessions/{session_id}/worker/heartbeat`

Workers maintain their lease by sending periodic heartbeats[@codesessions-ingress][@db-code-sessions]. Each successful heartbeat extends the `worker_lease_expires_at` timestamp by the lease TTL (default 60 seconds).

**Grace Period**: Workers have a grace period (default 10 seconds) after lease expiration to reconnect and continue working before a new worker can take over.

## Diagnostics

`POST /v1/code/sessions/{session_id}/worker/diagnostics`

Workers can send diagnostic log events for troubleshooting[@codesessions-ingress][@db-code-sessions]. These events are tagged with a diagnostic source and persisted separately from session events.

## Epoch-Based Ownership

All write endpoints (state, events, delivery, heartbeat) require a matching `worker_epoch`[@worker-state-design]. This design prevents multiple workers from interfering:

1. First worker registers and gets epoch 1
2. Worker 1 operates normally with epoch 1
3. Worker 2 registers and bumps epoch to 2
4. Worker 1's subsequent write requests fail with 409 conflict
5. Worker 2 operates exclusively with epoch 2

The `/worker/register` and `/bridge` endpoints are the only ways to acquire a new epoch. Regular state updates cannot change the epoch.

## Bridge Endpoint

`POST /v1/code/sessions/{session_id}/bridge`

The bridge endpoint authenticates external Claude Code instances and registers them as workers in a single operation[@codesessions-ingress]. It validates bridge credentials, creates a new epoch, and returns worker connection details.

## Error Responses

Worker API errors follow the Anthropic-compatible error shape[@worker-state-design]:

- `401 authentication_error`: Missing or invalid ingress token
- `404 not_found_error`: Code session not found
- `400 invalid_request_error`: Malformed JSON, missing fields, invalid values
- `409 conflict_error`: Worker epoch mismatch or stale worker
- `500 api_error`: Database or unexpected server errors

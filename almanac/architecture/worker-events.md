---
title: "Worker Events"
summary: "Worker event delivery protocol, ACK semantics, and state machine for CCR v2."
topics: [architecture, worker-events, claude-code-runtime]
sources:
  - id: worker-events-delivery
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-events-delivery.md
  - id: worker-events-delivery-backend
    type: file
    path: docs/design/be/ccrv2/ccr-v2-worker-events-delivery-backend-design.md
  - id: worker-internal-events
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-internal-events.md
  - id: codesessions-ingress
    type: file
    path: internal/codesessions/ingress.go
  - id: codesessions-service
    type: file
    path: internal/codesessions/service.go
---

# Worker Events

The worker event system provides reliable bidirectional communication between the service and Claude Code runtime workers. It implements application-layer ACK semantics, epoch-bound delivery tracking, and replay mechanisms to handle worker restarts and network interruptions.

## Event Delivery Flow

Events flow from service to worker through SSE streaming and from worker to service through HTTP endpoints:

**Service → Worker** (inbound events):
1. Public session events are queued as [`code_session_inbound_events`][@worker-events-delivery-backend]
2. SSE stream writes events as `client_event` frames
3. Worker acknowledges receipt via delivery endpoint
4. Service tracks delivery status: `queued → sent → received → processing → processed`

**Worker → Service** (outbound events):
1. Worker calls [`POST /v1/code/sessions/{id}/worker/events`][@codesessions-ingress]
2. Service validates worker epoch
3. Service normalizes event payload and metadata
4. Service publishes to public session stream if applicable

## Delivery ACK Protocol

The delivery ACK endpoint at [`POST /v1/code/sessions/{id}/worker/events/delivery`][@codesessions-ingress] implements application-layer acknowledgements:

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

ACK statuses follow monotonic state progression per [`ccr-v2-api-worker-events-delivery.md`][@worker-events-delivery]:

- **received** - Worker SSE transport has received the event
- **processing** - Worker has begun processing the command
- **processed** - Worker has completed processing

The service tracks delivery status with timestamps (`received_at`, `processing_at`, `processed_at`) and attempt counters. Unknown events or events not yet sent by the current epoch are ignored rather than rejected, preventing ACK retry loops.

## Event ID Association

Delivery ACKs associate worker-side event IDs with service-side events through payload UUID matching. The SSE frame's `event_id` field must equal the payload `uuid` for ACK correlation to work correctly.

Per [`ccr-v2-worker-events-delivery-backend-design.md`][@worker-events-delivery-backend]:

```text
StreamClientEvent.event_id = payload.uuid
```

This ensures that `received` ACKs (from SSE frames) and `processing`/`processed` ACKs (from command lifecycle) reference the same event.

## SSE Stream Replay

The SSE stream at [`GET /v1/code/sessions/{id}/worker/events/stream`][@codesessions-ingress] supports replay through sequence-based cursors:

- **`from_sequence_num` query parameter** - Resumes from specific sequence
- **`Last-Event-ID` header** - SSE standard replay cursor
- **Epoch-scoped replay** - Only events not yet `processed` are resent

When a new worker registers (bumping the epoch), the stream replays unprocessed events from the beginning, allowing worker state recovery after restarts.

## Internal Events

Worker transcript events use a separate persistence channel at [`/v1/code/sessions/{id}/worker/internal-events`][@codesessions-ingress]. Per [`ccr-v2-api-worker-internal-events.md`][@worker-internal-events], these events:

- Store Claude Code transcript messages for resume
- Support compaction boundaries for efficient storage
- Are scoped by agent ID for subagent transcripts
- Use sequence numbers for stable ordering

Internal events are retrieved during worker resume for transcript reconstruction. The GET endpoint supports pagination with compaction filtering, returning only events from the most recent compaction boundary forward.

## Worker Output Events

Worker output events are submitted through [`POST /v1/code/sessions/{id}/worker/events`][@codesessions-ingress] with envelope structure:

```json
{
  "worker_epoch": 1,
  "events": [
    {
      "payload": { "type": "text_delta", "text": "Hello" },
      "ephemeral": false
    }
  ]
}
```

Events are processed via [`AppendWorkerOutputEventsForEpoch()`][@codesessions-service]:

1. Validate worker epoch in transaction
2. Normalize payload and build metadata
3. Handle `keep_alive` events separately
4. Process `control_request` events for tool permissions
5. Publish non-ephemeral events to public stream

## Tool Permission Requests

Tool permission requests flow through the control request subtype:

1. Worker sends `control_request` event with `can_use_tool` subtype
2. Service processes via [`handleToolPermissionRequest()`][@codesessions-service]
3. Service evaluates permission policy
4. Service sends `control_response` back to worker via inbound queue

This enables runtime tool permission decisions while maintaining epoch ownership.

## Heartbeat Events

Worker heartbeats use a dedicated endpoint at [`POST /v1/code/sessions/{id}/worker/heartbeat`][@codesessions-ingress]. Heartbeats:

- Validate worker epoch
- Update `worker_last_heartbeat_at` timestamp
- Extend `worker_lease_expires_at` by 60 seconds
- Return new lease expiration time

Heartbeat failures due to epoch mismatch or lease expiration cause the worker to be considered stale and require re-registration.

## OTLP Metrics and Logs

The service accepts OTLP metrics and logs through [`POST /v1/code/sessions/{id}/worker/otlp/metrics`][@codesessions-ingress] and [`/worker/otlp/logs`][@codesessions-ingress]. These endpoints:

- Validate optional worker epoch
- Touch worker activity timestamp
- Parse OTLP protobuf payloads
- Store metrics/logs for observability

OTLP requests can include worker epoch for activity tracking, but missing epochs are handled gracefully for compatibility.

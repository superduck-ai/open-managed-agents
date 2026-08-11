---
title: "Event Delivery ACK"
summary: "Application-layer acknowledgment mechanism for CCR v2 worker events, enabling delivery confirmation and progress tracking through a dedicated endpoint."
topics: [architecture, reliability]
sources:
  - id: delivery-api
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-events-delivery.md
  - id: delivery-migration
    type: file
    path: internal/db/migrations/00007_add_code_session_inbound_delivery_ack.sql
---

SSE (Server-Sent Events) provides one-way push from server to worker with no transport-level acknowledgment. The event delivery ACK endpoint adds application-level confirmation, allowing workers to report receipt and processing progress for each client event pushed to them.

## Endpoint Contract

Workers POST acknowledgments to `POST /v1/code/sessions/{session_id}/worker/events/delivery` [@delivery-api]:

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

* `worker_epoch` — Current worker epoch (must be positive; `0` rejected)
* `updates[]` — Batch of 1-64 ACKs per request
* `event_id` — Identifies the acknowledged client event (matches SSE event UUID)
* `status` — `received`, `processing`, or `processed`

The endpoint returns `200 OK` with applied/ignored counts even for unknown event IDs, preventing infinite retry loops on stale events [@delivery-api].

## Status State Machine

Delivery status progresses monotonically:

```
received → processing → processed
```

Database fields track timestamps for each state:

* `received_at` — Set when status is `received` or later
* `processing_at` — Set when status is `processing` or `processed`
* `processed_at` — Set only when status is `processed`
* `last_delivery_update_at` — Refreshed on any valid ACK [@delivery-migration]

Late-arriving lower statuses cannot rollback higher statuses. A `processed` ACK followed by a `received` ACK retains the `processed` state and earlier timestamps [@delivery-api].

## Worker-Side Triggers

Three distinct code paths in the Claude Code worker trigger ACKs, all converging on `CCRClient.reportDelivery(eventId, status)` [@delivery-api]:

1. **`received`** — SSE transport callback fires immediately when a `client_event` frame arrives
2. **`processing`** — Command lifecycle listener emits `started` for user commands
3. **`processed`** — Command lifecycle listener emits `completed` for user commands

The `received` status applies to every client event (user messages, control requests, control responses). The `processing` and `processed` statuses only apply to user commands, creating a granular progress signal for the actual work being performed.

## Batch Upload Reliability

Workers use `SerialBatchEventUploader` with these properties [@delivery-api]:

* **Batch size**: 64 ACKs per POST request
* **Queue limit**: 64 pending ACKs (backpressure blocks on overflow)
* **Concurrent requests**: 1 in-flight POST at a time
* **Retry behavior**: Infinite retry on failure (no `maxConsecutiveFailures` cap)
* **Exponential backoff**: 500ms → 30s with jitter; `429` responses use `Retry-After` header
* **Fire-and-forget**: `enqueue()` returns void; failures do not block the main command loop

The uploader runs asynchronously from command execution. Even if the upload queue saturates, command processing continues.

## Server-Side Processing

On receiving an ACK batch, the server validates and applies updates:

1. Validate JSON structure, `worker_epoch` format, and `updates` array length (1-64)
2. Reject `worker_epoch: 0` — must be a positive integer from registration
3. For each update:
   * Locate matching `code_session_inbound_events` row by `event_id`
   * Skip if event doesn't exist (return 200, count as `ignored`)
   * Skip if event's `delivery_worker_epoch` doesn't match request epoch
   * Skip if event hasn't been marked `sent` for this epoch yet
   * Apply monotonic status transition and update timestamps
4. Return `{"ok": true, "applied": N, "ignored": M}`

Unknown or stale events are silently ignored rather than returning errors, preventing ACK upload retries from blocking on events that may have been cleaned up or replayed from an old epoch [@delivery-api].

## Epoch Association

Each ACK records the `delivery_worker_epoch` that applied it. When a new worker registers and bumps the epoch, the server can distinguish ACKs from obsolete workers. A worker attempting to ACK for an old epoch receives a `409 Conflict` response, preventing late ACKs from contaminating the current session state [@delivery-api].

## Uses and Limitations

Delivery ACKs serve three purposes:

1. **Confirmation** — Without a `received` ACK, the server only knows an event was written to SSE, not that the worker saw it
2. **Progress tracking** — `processing` and `processed` states give visibility into command execution lifecycle
3. **Liveness signal** — Events stuck in `sent` or `received` without progression may indicate worker issues

The ACK endpoint does not trigger retransmission. Redelivery happens through SSE stream replay on worker reconnection, which filters for events not yet marked `processed` [@delivery-api].

Event IDs in ACKs originate from the SSE frame's `event_id` field, which the server populates from the event's `payload_uuid`. This linkage allows the `received` ACK (from SSE transport) and the `processing`/`processed` ACKs (from command lifecycle) to reference the same event even though they traverse different code paths.

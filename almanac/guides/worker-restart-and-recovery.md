---
title: "Worker Restart and Recovery"
summary: "Mechanisms for code session worker restart, state recovery, and event delivery consistency in Open Managed Agents."
topics: [architecture, reliability, workers]
sources:
  - id: ccr-worker-state
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-state.md
  - id: heartbeat-impl
    type: file
    path: docs/design/be/ccrv2/worker-heartbeat-server-implementation.md
  - id: events-delivery
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-events-delivery.md
  - id: internal-events
    type: file
    path: docs/design/be/ccrv2/ccr-v2-api-worker-internal-events.md
---

Code session workers support restart and recovery through epoch-based ownership, heartbeat-based lease renewal, delivery acknowledgment, and internal event persistence. These mechanisms ensure state consistency when workers reconnect after failures.

## Worker Epoch

Worker ownership uses an epoch counter stored in `code_sessions.current_worker_epoch`. The counter starts at 0 and increments with each new worker registration via `POST /v1/code/sessions/{session_id}/worker/register`[@ccr-worker-state]. All worker write requests must include their current epoch—requests with mismatched epochs receive `409 conflict_error` responses[@ccr-worker-state].

Worker registration uses `SELECT ... FOR UPDATE` to lock the code session row, increment the epoch, and return the new value[@ccr-worker-state]. This serializes concurrent registration attempts and ensures old workers cannot write after a new worker takes over.

## Heartbeat and Lease Renewal

Workers maintain liveness through `POST /v1/code/sessions/{session_id}/worker/heartbeat`. Successful heartbeats extend the lease by 60 seconds and update `worker_lease_expires_at`[@heartbeat-impl]. The heartbeat request includes `worker_epoch` for ownership validation.

Heartbeats succeed if:
- The provided epoch matches `current_worker_epoch`
- The lease has not exceeded a 10-second grace period after expiration[@heartbeat-impl]

Expired heartbeats return `410 session_expired` without updating any state. Mismatched epochs return `409 conflict_error`[@heartbeat-impl]. The heartbeat handler uses `SELECT ... FOR UPDATE` to serialize with registration and other state updates[@heartbeat-impl].

## Worker State Persistence

Workers persist lightweight state via `PUT /v1/code/sessions/{session_id}/worker`. The endpoint accepts `worker_status`, `requires_action_details`, and `external_metadata` fields[@ccr-worker-state]. State updates require epoch matching and are protected by row-level locking.

The `worker_status` field transitions between `idle`, `running`, and `requires_action`[@ccr-worker-state]. When `requires_action_details` is present, the status must be `requires_action`—otherwise the details field is cleared[@ccr-worker-state]. Public session and thread status sync with worker status on explicit updates[@ccr-worker-state].

Workers recover previously written metadata via `GET /v1/code/sessions/{session_id}/worker`[@ccr-worker-state]. This endpoint returns `external_metadata` without refreshing connection status or activity timestamps.

## Event Delivery Acknowledgment

The delivery acknowledgment endpoint `POST /v1/code/sessions/{session_id}/worker/events/delivery` tracks event processing progress[@events-delivery]. Workers report three status levels for each client event:

- `received` — Event reached the worker process
- `processing` — Worker began processing the command
- `processed` — Worker completed the command[@events-delivery]

The endpoint accepts batched updates with `worker_epoch` and an array of `{event_id, status}` objects[@events-delivery]. Events must be marked as `sent` by the current epoch before acknowledgment—otherwise they are ignored[@events-delivery].

Delivery status helps confirm message receipt, track processing progress, and detect stuck workers. Events without acknowledgment remain in `sent` or earlier states and may be redelivered on worker reconnection[@events-delivery].

## Internal Event Persistence

Workers persist transcript events privately via `POST /v1/code/sessions/{session_id}/worker/internal-events`[@internal-events]. These events are stored in `code_session_internal_events` and are not visible to frontend clients or published to public SSE streams.

Internal events include transcript entries with types `user`, `assistant`, `attachment`, and `system`[@internal-events]. Each event includes a payload with message content, parent references, and optional metadata. Events are identified by `payload.uuid` for idempotency and ordered by `sequence_num`[@internal-events].

Workers recover transcript history via `GET /v1/code/sessions/{session_id}/worker/internal-events`[@internal-events]. The endpoint supports cursor-based pagination and compaction filtering, returning events from the most recent compaction boundary for each scope (foreground and subagents)[@internal-events].

## Restart Recovery Sequence

When a worker restarts, it follows this recovery sequence:

1. Register new epoch via `POST /worker/register`
2. Recover metadata via `GET /worker`
3. Fetch internal events via `GET /worker/internal-events`
4. Begin processing queued inbound events from `GET /worker/events/stream`
5. Report delivery status for processed events

The new epoch invalidates the old worker's lease—subsequent writes from the old worker receive `409` responses. Unprocessed events from the previous epoch remain available for replay by the new worker[@events-delivery].

## Failure Detection

Worker failures are detected through:
- Missed heartbeats beyond lease expiration plus grace period
- Stuck events remaining in `sent`/`received`/`processing` states
- Client-side timeout for SSE delivery

Detection mechanisms are observational—the system does not automatically terminate workers based on these signals. New worker registration is the authority for ownership transfer.

## Transaction Consistency

All worker state operations use `SELECT ... FOR UPDATE` on the code session row to serialize concurrent modifications[@ccr-worker-state][@internal-events]. This ensures:
- Registration always bumps epoch before new state writes
- Heartbeats and state updates conflict with concurrent registration
- Event appends respect epoch ownership

The strong consistency boundary is the PostgreSQL row—no Redis or additional state is required for correctness[@heartbeat-impl].

---
title: "Epoch-Based Ownership"
summary: "Code session workers use per-session epoch counters to enforce exclusive ownership and prevent stale worker writes."
topics: [architecture, concurrency]
sources:
  - id: epoch-design
    type: file
    path: docs/design/be/ccrv2/ccr-v2-epoch-design.md
---

The CCR v2 worker ingress uses an epoch-based ownership mechanism to ensure that only the current registered worker can write to a code session. When a new worker registers or bridges, the session epoch advances, and subsequent write requests from the old worker return `409 Conflict` errors.

## Data Model

The `code_sessions` table stores epoch state in several columns. `current_worker_epoch` holds the per-session monotonic counter starting at `0`. `worker_lease_expires_at` tracks the lease deadline for the current epoch worker. `worker_registered_at` records when the current epoch began. `worker_last_heartbeat_at` tracks the most recent heartbeat. `worker_token_session_id` stores the session identifier from the worker token. `worker_binding` contains binding metadata without storing raw tokens [@epoch-design].

## Epoch Registration

Worker registration and bridge operations both increment the epoch. The `RegisterCodeSessionWorker` function executes within a single database transaction, locking the `code_sessions` row with `select ... for update`. It calculates `nextEpoch := session.current_worker_epoch + 1`, updates all worker-related fields, and returns the new epoch [@epoch-design].

This design ensures that concurrent registrations for the same code session serialize through row-level locking while different sessions can register concurrently without blocking. Any valid new registration can immediately claim ownership without waiting for lease expiration [@epoch-design].

## Write Request Validation

All worker write requests must include a `worker_epoch` parameter. Endpoints such as `PUT /worker`, `POST /worker/events`, and `POST /worker/heartbeat` validate the provided epoch against the current session epoch [@epoch-design].

Missing or invalid epoch values return `400 invalid_request_error`. Unknown code sessions return `404 not_found_error`. Epoch mismatches return `409 conflict_error`. This forces old workers to exit when a new worker claims ownership [@epoch-design].

## Transactional Protection

Critical protection occurs at the database transaction level. The epoch check must happen within the same transaction and under the same row lock as the event insertion. A preliminary check in the HTTP handler is insufficient due to race conditions between validation and write [@epoch-design].

The `appendCodeSessionEvent()` function uses `RequiredWorkerEpoch *int64` to re-check the epoch after acquiring the row lock. If the epoch has changed since the HTTP handler checked, the transaction returns `ErrWorkerEpochMismatch` and no event is written [@epoch-design].

## Heartbeat and Lease

Heartbeat operations renew the lease for the current epoch without bumping the counter. The conditional update includes `current_worker_epoch = $epoch` so that heartbeats from old epochs fail silently. Lease expiration alone does not advance the epoch—only a new registration or bridge operation increments it [@epoch-design].

## Read Path Behavior

Read paths such as `GET /worker` and `GET /worker/events/stream` can optionally validate epochs. If a request provides an epoch that does not match, the endpoint returns `409`. Without an epoch parameter, read paths return current state without modifying connection status or activity timestamps [@epoch-design].

State writes on read paths require epoch-scoped conditional updates. Status writes from old epochs cannot overwrite current worker state due to the `current_worker_epoch = $epoch` clause in the update conditions [@epoch-design].

## API Contract

Registration and bridge responses return the new epoch as a string to maintain JavaScript safe integer compatibility while supporting future growth. The `POST /bridge` response also includes `worker_jwt`, `worker_token`, `worker_token_type`, `api_base_url`, and `expires_in` fields [@epoch-design].

Each `/bridge` call bumps the epoch even if an active worker exists. This is necessary for proper takeover semantics—reusing an existing epoch would prevent the new worker from claiming exclusive ownership [@epoch-design].

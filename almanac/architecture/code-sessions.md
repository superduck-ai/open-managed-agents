---
title: "Code Sessions"
summary: "Claude Code runtime session management, worker ingress, and epoch-based ownership."
topics: [architecture, code-sessions, claude-code-runtime]
sources:
  - id: codesessions-service
    type: file
    path: internal/codesessions/service.go
  - id: codesessions-ingress
    type: file
    path: internal/codesessions/ingress.go
  - id: codesessions-events
    type: file
    path: internal/codesessions/events.go
  - id: epoch-design
    type: file
    path: docs/design/be/ccrv2/ccr-v2-epoch-design.md
---

# Code Sessions

Code sessions provide the execution environment for Claude Code runtime integration with managed agents. They implement the CCR v2 protocol with epoch-based worker ownership, bidirectional event streaming, and bridge authentication for connecting worker processes to the service API.

## Code Session Structure

A [`CodeSession`][@codesessions-service] represents a Claude Code runtime instance with the following properties:

- **External ID** - Public-facing session identifier (`cse_*` prefix)
- **Session association** - Links to the public managed agent session
- **Environment context** - Associated environment and work directory
- **Worker state** - Current worker epoch, status, and binding metadata
- **Permission mode** - Tool permission policy for the session
- **Model configuration** - Model selection and system prompt settings

Code sessions are created through [`CreateManagedAgentCodeSession()`][@codesessions-service] when a managed agent needs Claude Code runtime capabilities. The session stores metadata including the public session ID, environment ID, title, and configuration.

## Worker Registration and Epochs

Code sessions use an epoch-based ownership model per [`ccr-v2-epoch-design.md`][@epoch-design]. Worker registration through [`POST /v1/code/sessions/{id}/worker/register`][@codesessions-ingress] increments the epoch and returns worker credentials:

```go
epoch, _, err := s.db.RegisterCodeSessionWorker(r.Context(), codeSessionID, binding, codeSessionWorkerLeaseTTL)
```

The epoch is a per-code-session monotonic counter starting at 1. Each successful registration increments the counter, invalidating previous workers. Worker write requests must include `worker_epoch` and are rejected with `409 conflict_error` if the epoch doesn't match.

## Worker Authentication

Code sessions support multiple authentication modes:

1. **Session ingress token** - Legacy token-only mode where the code session ID serves as token
2. **Bridge bearer** - [`POST /v1/code/sessions/{id}/bridge`][@codesessions-ingress] authenticates via API key or platform session and returns worker credentials with elevated epoch

The bridge endpoint enables external systems to obtain worker credentials for a code session while maintaining authentication traceability through the [`codeSessionBridgeWorkerBinding()`][@codesessions-ingress] metadata.

## Event Ingestion

Code session events flow from worker to service through multiple endpoints:

- **WebSocket** - [`/v1/session_ingress/ws/{id}`][@codesessions-ingress] for real-time event streaming
- **HTTP** - [`POST /v1/session_ingress/session/{id}/events`][@codesessions-ingress] for batch event submission
- **Worker events** - [`POST /v1/code/sessions/{id}/worker/events`][@codesessions-ingress] with epoch validation

Events are processed via [`AppendWorkerEvent()`][@codesessions-service] which normalizes payloads, builds event metadata, and persists inbound events. The [`BuildEventMetadata()`][@codesessions-events] function extracts event type, UUID, and request ID for idempotency tracking.

## Event Delivery to Workers

Service-to-worker event delivery uses SSE streaming via [`GET /v1/code/sessions/{id}/worker/events/stream`][@codesessions-ingress]. The stream:

1. Validates worker epoch (if provided)
2. Parses `from_sequence_num` or `Last-Event-ID` for replay cursor
3. Marks worker as connected for the epoch
4. Streams inbound events as `client_event` SSE frames
5. Handles disconnect by marking worker disconnected

The [`streamCodeSessionWorkerEvents()`][@codesessions-ingress] function implements the polling loop with keepalive frames and sequence-based replay.

## Worker State Management

Worker state is managed through [`GET/PUT /v1/code/sessions/{id}/worker`][@codesessions-ingress]:

- **GET** - Returns current worker state including status, epoch, and metadata
- **PUT** - Updates worker status, requires action details, and external metadata

Worker status values include `idle`, `running`, and `requires_action`. Status changes are synchronized with public session status via [`syncPublicSessionStatusFromWorker()`][@codesessions-ingress].

## Heartbeat and Lease Management

Worker liveness is maintained through [`POST /v1/code/sessions/{id}/worker/heartbeat`][@codesessions-ingress]. The heartbeat:

1. Validates worker epoch matches current session epoch
2. Updates `worker_last_heartbeat_at` timestamp
3. Extends `worker_lease_expires_at` by 60 seconds
4. Returns new lease expiration time

Lease expiration doesn't automatically bump the epoch but causes subsequent heartbeats to fail with [`db.ErrWorkerLeaseExpired`][@codesessions-ingress]. New worker registration is required to resume activity.

## Internal Events

Worker transcript events are persisted through the internal events endpoint at [`/v1/code/sessions/{id}/worker/internal-events`][@codesessions-ingress]. These events:

- Store Claude Code transcript messages (`user`, `assistant`, `attachment`, `system`)
- Support compaction boundaries for efficient resume
- Are scoped by agent ID for subagent transcripts
- Use sequence numbers for stable ordering

Internal events are retrieved during worker resume for transcript reconstruction.

## Bridge Authentication Flow

The bridge authentication flow allows external systems to obtain worker credentials:

1. External system authenticates with API key or platform session
2. Calls [`POST /v1/code/sessions/{id}/bridge`][@codesessions-ingress]
3. Service validates access to the associated public session
4. Service creates worker binding with credential metadata
5. Service increments epoch and returns worker token and epoch
6. External system uses worker token for subsequent operations

This flow maintains authentication traceability while enabling secure worker credential distribution.

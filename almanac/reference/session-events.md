---
title: "Session Events"
summary: "Session events record all activity within a managed agent session, including user messages, tool calls, status changes, and coordination across threads."
topics: [reference, sessions, events]
sources:
  - id: event-payload
    type: file
    path: internal/sessions/event_payload.go
  - id: managedagentsevents-events
    type: file
    path: internal/managedagentsevents/events.go
  - id: db-sessions
    type: file
    path: internal/db/sessions.go
  - id: sessions-fixtures
    type: file
    path: internal/sessions/fixtures.go
---

# Session Events

Session events are time-ordered records of all activity within a managed agent session. Each event has a type, payload, and timestamps. Events are stored in the `session_events` table and can be streamed to clients via SSE or retrieved through pagination[@db-sessions].

## Event Categories

The `managedagentsevents` package categorizes events into logical groups[@managedagentsevents-events]:

- **CategoryInput**: User-initiated events like `user.message`, `user.interrupt`, `user.tool_confirmation`, `user.custom_tool_result`
- **CategoryAgent**: Agent-side events like `agent.message`, `agent.thinking`, `agent.thread_context_compacted`
- **CategoryTool**: Tool execution events like `agent.tool_use`, `agent.tool_result`, `agent.mcp_tool_use`, `agent.custom_tool_use`
- **CategorySessionStatus**: Session lifecycle events like `session.status_running`, `session.status_idle`, `session.status_terminated`
- **CategoryThreadStatus**: Thread-specific status events like `session.thread_status_running`, `session.thread_status_idle`
- **CategoryThreadCoordination**: Multi-thread events like `session.thread_created`, `agent.thread_message_received`, `agent.thread_message_sent`
- **CategorySpan**: Tracing events like `span.model_request_start`, `span.outcome_evaluation_start`
- **CategorySystem**: System messages like `system.message`

Events are considered "persisted managed agent events" if they belong to a known category, excluding unknown categories and stream deltas[@managedagentsevents-events].

## Event Payload Structure

Each event's payload is stored as JSONB in the `session_events` table[@db-sessions]. The `sessionEventPayload` function in `internal/sessions/event_payload.go` normalizes payloads for API responses[@event-payload]:

- Adds `created_at` and `processed_at` timestamps if not already present
- Includes `session_thread_id` for thread-scoped events when responding to thread-scoped queries
- Preserves all other payload fields unchanged

The payload normalization ensures clients receive consistent timestamp formats regardless of how events were ingested.

## Public Session History

Not all events are exposed in the public session history API. The `IsPublicSessionHistoryEvent` function determines visibility[@managedagentsevents-events]:

- Stream delta events (`event_start`, `event_delta`) are excluded
- `env_manager_log` events are excluded
- All other persisted managed agent events are included
- Claude Code transcript events (`assistant`, `user`, `system`, `result`) are included

This filtering allows internal operational events to be logged without exposing them to API clients.

## Status Transitions

Session events drive status transitions through specific event types[@managedagentsevents-events]:

- `session.status_run_started`, `session.running` → session enters "running" status
- `session.status_idle`, `session.idled`, `session.requires_action` → session enters "idle" status
- `session.status_rescheduled` → session enters "rescheduling" status
- `session.status_terminated`, `session.deleted` → session enters "terminated" status

Similar mappings exist for thread status events. The system uses these mappings to update the `sessions` and `session_threads` tables' status columns as events are processed.

## Cross-Posted Blocking Events

Tool use events that require user approval are "blocking events" that pause session execution[@managedagentsevents-events]. These include:

- `agent.tool_use`: Agent toolset tool calls (bash, edit, read, etc.)
- `agent.mcp_tool_use`: MCP server tool calls
- `agent.custom_tool_use`: Custom tool invocations

When a blocking event is emitted, the session transitions to idle with `stop_reason.type="requires_action"`. The associated event IDs are listed in `stop_reason.event_ids` so clients know which approvals are pending.

## Tool Result and Confirmation Events

Tool result events (`agent.tool_result`, `agent.mcp_tool_result`, `user.tool_result`) and confirmation events (`user.tool_confirmation`) carry `tool_use_id` fields that link them to the original tool use request[@event-payload]. The system uses these references to:

- Match results to their originating tool use events
- Route confirmation responses to the correct tool permission request
- Handle tool execution across thread boundaries in multi-threaded sessions

Special handling exists for "orphan" tool result events that arrive in the primary thread before their corresponding child thread's tool use event is known to the system.

## Client Input Events

Client input events are those that originate from external API calls rather than from the agent or system[@managedagentsevents-events]. These include:

- `user.message`: User text messages
- `user.interrupt`: User interruption requests
- `user.custom_tool_result`: Results from custom tool execution by the client
- `user.tool_confirmation`: Approval/denial responses for tool use requests
- `user.define_outcome`: Outcome evaluation submissions
- `user.tool_result`: Legacy tool result events
- `system.message`: System-level messages (rare)

Only client input events are accepted through the public events API endpoints; all other event types are generated internally.

## Event Pagination and Ordering

Events are paginated using a cursor that combines `created_at` and the database row ID[@event-payload]. The `encodeEventCursor` and `decodeEventCursor` functions create and parse these cursors, which support both ascending and descending order traversal.

The cursor-based pagination ensures stable ordering even when multiple events have identical timestamps, and it allows efficient resumption of event streams without skipping or duplicating records.

## Fixture Events

For SDK testing purposes, the sessions package includes fixture generation logic in `internal/sessions/fixtures.go`[@sessions-fixtures]. The `normalizeFixtureEvent` function generates well-formed event payloads with proper `id` and `processed_at` fields, ensuring official SDK tests have consistent event data to work with.

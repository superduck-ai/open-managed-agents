---
title: "Session UI"
summary: "Session detail page displays agent execution events with lane-based timeline, minimap navigation, and transcript/debug view modes."
topics: [architecture]
sources:
  - id: session-detail-page
    type: file
    path: web/src/features/managed-agents/sessions/SessionDetailPage.tsx
  - id: session-detail-model
    type: file
    path: web/src/features/managed-agents/sessions/sessionDetailModel.ts
  - id: session-timeline
    type: file
    path: web/src/features/managed-agents/sessions/sessionTimeline.tsx
  - id: session-trace-panel
    type: file
    path: web/src/features/managed-agents/sessions/SessionTracePanel.tsx
  - id: session-trace-rows
    type: file
    path: web/src/features/managed-agents/sessions/sessionTraceRows.tsx
  - id: session-trace-model
    type: file
    path: web/src/features/managed-agents/sessions/sessionTraceModel.ts
  - id: session-detail-data
    type: file
    path: web/src/features/managed-agents/sessions/sessionDetailData.ts
  - id: session-lane-design
    type: file
    path: docs/design/fe/sessions/session-detail-lane-timeline-design.md
  - id: session-tool-display
    type: file
    path: docs/design/fe/sessions/session-tool-call-display.md
  - id: session-tool-permission
    type: file
    path: docs/design/fe/sessions/session-tool-permission-confirmation.md
---

## Session UI

The session detail page provides a comprehensive view of agent execution events with a lane-based timeline for multi-threaded sessions, minimap navigation, and dual view modes for transcript and debug inspection. The implementation follows the lane timeline design spec with percentage-based coordinates for event visualization [@session-lane-design].

## Page Structure

The `SessionDetailPage` component renders a full-featured interface for session inspection [@session-detail-page]:
- **Header**: Session title, status pill, metadata chips, and action buttons
- **Controls bar**: View mode toggle, event type filters, and search
- **Events minimap**: Horizontal timeline showing event density and position
- **Lane tabs**: Vertical thread selector for multi-agent sessions
- **Event list**: Scrollable transcript or debug event rows
- **Detail panel**: Side panel showing selected event content

## Event Data Model

Session events are fetched from the session events API and transformed into display entries. The `SessionDetailDeltaFramesContext` provides streaming event updates for live sessions [@session-detail-data].

Events are normalized into `SessionEventListEntry` types:
- **Transcript entries**: Human-readable consolidated events (user messages, agent responses, tool calls with results)
- **Debug entries**: Raw event stream with full metadata

The transformation from raw events to display entries handles tool use/result pairing, permission confirmation folding, and bracket-based duration calculation [@session-trace-model].

## Timeline Visualization

The events minimap displays a horizontal representation of session events using percentage-based coordinates [@session-timeline]. Key features:

- **Lane-based layout**: Each thread occupies a horizontal lane
- **Percentage positioning**: Events positioned by `leftPct` and `widthPct` for responsive scaling
- **Logarithmic compression**: Long durations compressed to prevent excessive width
- **Seek interaction**: Click to select events and scroll the list into view

The timeline model processes events through `buildSessionTimeline` which creates tick data with compressed coordinates for rendering.

## Lane Model

Multi-agent sessions display multiple lanes representing different threads [@session-detail-model]:
- **Main lane**: Primary session thread (lane 0)
- **Thread lanes**: Child agent threads ordered by creation time
- **Archived lanes**: Hidden by default, toggleable via UI

The `buildSessionDetailLaneState` function creates the lane structure with deduplicated agent names (e.g., "Researcher", "Researcher 2") and thread-to-lane mapping.

## View Modes

Two view modes provide different levels of event inspection:

**Transcript mode** shows human-readable consolidated events:
- User messages and agent responses
- Tool calls with inline result/status indicators
- Error messages and session outcomes
- Permission confirmation status folded into tool call rows

**Debug mode** shows the raw event stream:
- Individual tool use and tool result events
- User tool confirmation events
- Streaming passthrough events
- Full event metadata

## Event Display

Events are rendered as rows with type-specific content [@session-trace-rows]:

- **TranscriptRow**: Consolidated event with icon, content preview, and metadata strip
- **DebugRow**: Raw event JSON with event type badge

Tool calls receive special handling per the tool call display spec [@session-tool-display]:
- Tool name beautification (strip prefixes, capitalize parts)
- Input preview from highest-priority field
- Lifecycle badge (awaiting approval, running, failed, denied, completed)
- Execution duration and token usage metadata

## Event Selection and Detail

Selecting an event opens a detail panel showing full event content [@session-trace-panel]. The detail panel supports:
- **Content tab**: Formatted event content with syntax highlighting for code
- **Deltas tab**: Request/response deltas for tool calls
- **JSON tab**: Raw event payload

Tool call details include the tool use input, result content, and permission confirmation status per the tool permission confirmation spec [@session-tool-permission].

## Filtering and Search

Events can be filtered by:
- **Event type**: Checkboxes for user, agent, tool, error, and outcome events
- **Text search**: Full-text search across event content
- **Lane scope**: Events from selected thread or all threads

Filtered entries update the minimap visible IDs to reflect the current view subset.

## Streaming and Live Updates

For active sessions, the `useSessionDetailEventData` hook establishes an SSE connection to receive live events [@session-detail-data]. The stream:
- Updates the session status based on event types
- Refreshes thread list on thread creation events
- Auto-scrolls to new events when near bottom
- Preserves scroll position when user scrolls up

## Thread Navigation

Cross-thread navigation allows jumping between related events:
- Thread message sent/received badges link to counterpart events
- Lane tab switching changes the active event source
- Archived lane expansion reveals hidden threads

The `handleThreadClick` function locates counterpart events by type and timestamp proximity.

## URL State

Session detail state is encoded in URL parameters for shareability:
- `?segment=debug`: Switch to debug view mode
- `?event={id}`: Select specific event on load
- Lane and archived preferences persisted to storage

The `writeSessionDetailUrlState` function keeps URL in sync with UI state.

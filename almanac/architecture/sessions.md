---
title: "Sessions Architecture"
summary: "Session lifecycle, event streaming, and SSE implementation for managed agent conversations."
topics: [architecture, sessions, streaming]
sources:
  - id: sessions-handler
    type: file
    path: internal/sessions/handler.go
  - id: sessions-service
    type: file
    path: internal/sessions/service.go
  - id: sessions-stream-hub
    type: file
    path: internal/sessions/stream_hub.go
  - id: sessions-event-mapper
    type: file
    path: internal/sessions/event_mapper.go
---

# Sessions Architecture

The sessions system provides the core conversational interface for managed agents. It handles session lifecycle, event persistence, thread management, and server-sent events (SSE) streaming. The architecture separates session management from code session execution while maintaining event flow between public API events and worker-internal processing.

## Session Structure

A [`Session`][@sessions-service] represents a managed agent conversation with the following core elements:

- **Session record** - Top-level conversation with agent, environment, and status
- **Session threads** - Conversation threads (primary for main dialog, subagent threads for delegated tasks)
- **Session events** - Immutable event log with ordering and delivery tracking
- **Session resources** - Attached resources like GitHub repositories
- **Environment work** - Queued work items for environment execution

Sessions are created via [`POST /v1/sessions`][@sessions-handler] with agent specification, environment ID, optional title, metadata, and vault references. The creation process atomically creates the session, primary thread, and initial work item within a single database transaction.

## Thread Management

Sessions contain one or more [`SessionThread`][@sessions-service] records:

- **Primary thread** - Main conversation thread, auto-created on session creation
- **Subagent threads** - Created when agents spawn sub-tasks or delegated conversations

The [`ensurePrimarySessionThread()`][@sessions-event-mapper] function guarantees a primary thread exists. Subagent threads are created through [`ensureSessionThread()`][@sessions-event-mapper] when events reference thread IDs that don't exist. Thread hierarchy is maintained via `parent_thread_id` linking.

## Event Persistence

Session events are written through [`AppendSessionEvents()`][@sessions-handler] in [`db.CreateSessionInput`][@sessions-service]. Events carry:

- Event type discriminator (e.g., `user.message`, `agent.tool_use`)
- Payload containing event data
- Optional thread association
- Creation and processing timestamps
- Ordering sequence for consistency

Events serve as the source of truth for conversation history and drive downstream processing including webhook delivery, code session event forwarding, and stream broadcasting.

## Code Session Event Flow

The architecture supports bidirectional event flow between public sessions and code sessions (Claude Code runtime). Public session events flow to code sessions via [`QueuePublicSessionEvents()`][@sessions-service] in the sessions handler, while code session worker events are mapped back to public events through the event system.

The [`streamDeltaEventFromCodeSessionPayload()`][@sessions-event-mapper] and [`sessionEventsFromCodeSessionPayload()`][@sessions-event-mapper] functions handle the translation between code session event formats and public session event formats. Stream delta events are broadcast immediately without persistence, while substantive events are persisted and then broadcast.

## SSE Streaming

Session events are streamed to clients via server-sent events through the [`streamHub`][@sessions-stream-hub] mechanism:

1. Client connects to [`GET /v1/sessions/{session_id}/events/stream`][@sessions-stream-hub]
2. Handler authorizes session access and ensures primary thread exists
3. [`subscribe()`][@sessions-stream-hub] creates a channel for the session/thread
4. [`broadcast()`][@sessions-stream-hub] distributes events to matching subscribers
5. [`writeSSE()`][@sessions-stream-hub] formats events as SSE frames

The streaming hub filters events by session ID, thread ID, and event type. Subscribers can request stream deltas (partial response chunks) via the `event_deltas[]` query parameter.

## Event Filtering and Routing

The event system includes sophisticated routing logic for determining event placement:

- **Primary thread placement** - Default for coordination events and unthreaded events
- **Owner thread inference** - Events with agent/task IDs are routed to corresponding subagent threads
- **Cross-posting** - Blocking tool events are posted to both thread and primary for visibility
- **Thread status events** - Thread lifecycle events are associated with their respective threads

The [`sessionEventCopySpecs()`][@sessions-event-mapper] function implements this logic, analyzing event types and payload content to determine appropriate thread placement.

## Status Management

Sessions support multiple statuses: `idle`, `running`, `rescheduling`, `terminated`. Status transitions trigger webhook events and affect which operations are allowed. For example, running sessions cannot be archived or deleted.

Thread status mirrors session status by default but can diverge during complex multi-agent scenarios. The [`threadStatusForSession()`][@sessions-event-mapper] function provides default status mapping.

## Webhook Integration

Session lifecycle changes trigger webhook events via the [`webhooks.Enqueue()`][@sessions-handler] calls. Events include `session.created`, `session.pending`, `session.status_idled`, `session.thread_created`, `session.thread_terminated`, and `session.archived`. These webhooks enable external systems to react to conversation state changes.

## Resource Management

Session resources (currently GitHub repositories) are managed through CRUD operations on the [`/v1/sessions/{session_id}/resources`][@sessions-handler] endpoint. Resources carry authorization tokens and are associated with sessions for agent access during execution.

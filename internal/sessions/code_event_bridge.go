package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (h *Handler) PublishProcessedInput(ctx context.Context, codeSession db.CodeSession, eventID string) error {
	// Worker payload IDs are not always public inputs; only stored inputs are acknowledged.
	original, err := h.eventPayloads.GetSessionEvent(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID, eventID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && !original.ProcessedAt.IsZero()) {
		return nil
	}
	if err != nil {
		return err
	}
	processed, changed, err := h.db.MarkSessionEventProcessed(ctx, codeSession, eventID, eventTime(time.Now()))
	if err != nil || !changed {
		return err
	}
	original.ProcessedAt = processed.ProcessedAt
	original.Payload = sessionEventPayload(original)
	h.publishSessionEvents(ctx, []db.SessionEvent{original})
	return nil
}

func (h *Handler) PublishCodeSessionEvents(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) error {
	if h == nil || len(payloads) == 0 {
		return nil
	}
	session, found, err := h.db.GetSession(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
	if err != nil {
		return err
	}
	if !found {
		return db.ErrNotFound
	}
	var streamEvents []db.SessionEvent
	var events []db.SessionEvent
	now := time.Now().UTC()
	for _, raw := range payloads {
		eventType := rawSessionEventType(raw)
		if maevents.IsStreamDelta(eventType) {
			event, err := h.streamDeltaEventFromCodeSessionPayload(ctx, session, codeSession.ExternalID, raw, now)
			if err != nil {
				h.logger.WarnContext(ctx, "skip code session stream delta", "session_id", session.ExternalID, "code_session_id", codeSession.ExternalID, "error", err)
				continue
			}
			streamEvents = append(streamEvents, event)
			continue
		}
		batch, err := h.sessionEventsFromCodeSessionPayload(ctx, session, codeSession.ExternalID, raw, now)
		if err != nil {
			if maevents.IsPersistedManagedAgentEvent(eventType) {
				return err
			}
			h.logger.WarnContext(ctx, "skip code session event", "session_id", session.ExternalID, "code_session_id", codeSession.ExternalID, "error", err)
			continue
		}
		events = append(events, batch...)
	}
	h.publishSessionEvents(ctx, streamEvents)
	if len(events) == 0 {
		return nil
	}
	created, err := h.eventPayloads.AppendSessionEventsIfAbsent(ctx, session.WorkspaceUUID, session.ExternalID, events)
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return nil
		}
		return err
	}
	h.publishSessionEvents(ctx, created)
	h.enqueueWebhooksForSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, created)
	return nil
}

func (h *Handler) appendAndBroadcastInternal(r *http.Request, sessionID string, events []db.SessionEvent) {
	created, err := h.eventPayloads.AppendSessionEvents(r.Context(), workspaceUUIDFromRequest(r), sessionID, events, nil)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "append internal session events", "session_id", sessionID, "error", err)
		return
	}
	h.publishSessionEvents(r.Context(), created)
}

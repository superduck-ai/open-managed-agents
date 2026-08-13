package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (h *Handler) appendAndBroadcastInternal(r *http.Request, sessionID string, events []db.SessionEvent) {
	created, err := h.db.AppendSessionEvents(r.Context(), workspaceUUIDFromRequest(r), sessionID, events, nil)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "append internal session events", "session_id", sessionID, "error", err)
		return
	}
	for _, event := range created {
		h.broadcast(event)
	}
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
	var events []db.SessionEvent
	now := time.Now().UTC()
	for _, raw := range payloads {
		if maevents.IsStreamDelta(rawSessionEventType(raw)) {
			event, err := h.streamDeltaEventFromCodeSessionPayload(ctx, session, codeSession.ExternalID, raw, now)
			if err != nil {
				h.logger.WarnContext(ctx, "skip code session stream delta", "session_id", session.ExternalID, "code_session_id", codeSession.ExternalID, "error", err)
				continue
			}
			h.broadcastStreamDelta(event)
			continue
		}
		batch, err := h.sessionEventsFromCodeSessionPayload(ctx, session, codeSession.ExternalID, raw, now)
		if err != nil {
			h.logger.WarnContext(ctx, "skip code session event", "session_id", session.ExternalID, "code_session_id", codeSession.ExternalID, "error", err)
			continue
		}
		events = append(events, batch...)
	}
	if len(events) == 0 {
		return nil
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].ProcessedAt.Equal(events[j].ProcessedAt) {
			return events[i].ProcessedAt.Before(events[j].ProcessedAt)
		}
		if !events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].CreatedAt.Before(events[j].CreatedAt)
		}
		return events[i].ExternalID < events[j].ExternalID
	})
	created, err := h.db.AppendSessionEventsIfAbsent(ctx, session.WorkspaceUUID, session.ExternalID, events)
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return nil
		}
		return err
	}
	for _, event := range created {
		h.applySessionEventEffects(ctx, event)
		h.broadcast(event)
	}
	h.enqueueWebhooksForSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, created)
	return nil
}

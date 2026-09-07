package sessions

import (
	"context"
	"encoding/json"
	"net/http"
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
	h.publishSessionEvents(r.Context(), created)
}

// AppendCodeSessionEvents maps and writes through the caller's locked transaction.
// Returned events are safe to notify only after that transaction commits.
func (h *Handler) AppendCodeSessionEvents(ctx context.Context, tx db.ManagedAgentEventTx, session db.Session, codeSessionID string, payloads []json.RawMessage) ([]db.SessionEvent, error) {
	var created []db.SessionEvent
	now := time.Now().UTC()
	for _, raw := range payloads {
		if rawSessionEventType(raw) == "session.error" {
			if err := validateSessionErrorEvent(raw); err != nil {
				return nil, err
			}
		}
		var usage json.RawMessage
		if rawSessionEventType(raw) == "session.usage" {
			var err error
			usage, err = decodeSessionUsageEvent(raw)
			if err != nil {
				return nil, err
			}
		}
		if maevents.IsStreamDelta(rawSessionEventType(raw)) {
			event, err := h.streamDeltaEventFromCodeSessionPayload(ctx, tx, session, codeSessionID, raw, now)
			if err != nil {
				return nil, err
			}
			created = append(created, event)
			continue
		}
		batch, err := h.sessionEventsFromCodeSessionPayload(ctx, tx, session, codeSessionID, raw, now)
		if err != nil {
			return nil, err
		}
		for i := range batch {
			batch[i].StateChange = sessionEventStateChange(batch[i])
		}
		if _, status := maevents.ThreadStatus(rawSessionEventType(raw)); status {
			for _, event := range batch {
				inserted, err := appendThreadStatusEvent(ctx, tx, session, codeSessionID, event)
				if err != nil {
					return nil, err
				}
				created = append(created, inserted...)
			}
			continue
		}
		inserted, err := tx.AppendSessionEventsIfAbsent(ctx, session, batch, []string{"created_at", "processed_at", "timestamp"})
		if err != nil {
			return nil, err
		}
		// Usage is a complete cumulative snapshot. Only a newly committed event
		// replaces the projection; retrying an older ID must not rewind it.
		if len(usage) > 0 && len(inserted) > 0 {
			if err := tx.SetSessionUsage(ctx, session, usage); err != nil {
				return nil, err
			}
		}
		created = append(created, inserted...)
	}
	return created, nil
}

func (h *Handler) NotifyCodeSessionEvents(ctx context.Context, events []db.SessionEvent) {
	if len(events) == 0 {
		return
	}
	h.publishSessionEvents(ctx, events)
	h.enqueueWebhooksForSessionEvents(ctx, events[0].WorkspaceUUID, events[0].SessionExternalID, events)
}

package sessions

import (
	"context"
	jsonv2 "encoding/json/v2"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

// Input acceptance activates the turn before its user messages are published.
// The append transaction decides whether these status transitions are needed.
func (h *Handler) prependInputRunningEvents(ctx context.Context, session db.Session, events []db.SessionEvent) ([]db.SessionEvent, error) {
	for _, event := range events {
		if event.EventType != "user.message" {
			continue
		}
		primary, err := h.ensurePrimarySessionThread(ctx, session)
		if err != nil {
			return nil, err
		}
		if event.ThreadExternalID != nil && *event.ThreadExternalID != primary.ExternalID {
			continue
		}
		payload, err := jsonv2.Marshal(struct {
			ID          string    `json:"id"`
			Type        string    `json:"type"`
			CreatedAt   time.Time `json:"created_at"`
			ProcessedAt time.Time `json:"processed_at"`
		}{event.ExternalID + "_running", "session.status_running", event.CreatedAt, event.CreatedAt})
		if err != nil {
			return nil, err
		}
		running, err := h.sessionEventsFromCodeSessionPayload(ctx, session, session.ExternalID, payload, event.CreatedAt)
		if err != nil {
			return nil, err
		}
		for i := range running {
			running[i].InputEventID = event.ExternalID
		}
		return append(running, events...), nil
	}
	return events, nil
}

func (h *Handler) sessionUpdatedEvent(session db.Session) (db.SessionEvent, error) {
	eventID, err := ids.New("sevt_")
	if err != nil {
		return db.SessionEvent{}, err
	}
	now := time.Now().UTC()
	payload, err := httpapi.MarshalRaw(map[string]any{
		"id":           eventID,
		"agent":        agentsnapshot.RawJSONValue(session.AgentSnapshot, nil),
		"created_at":   httpapi.FormatTime(now),
		"metadata":     agentsnapshot.RawJSONValue(session.Metadata, map[string]any{}),
		"processed_at": now.Format(time.RFC3339),
		"title":        session.Title,
		"type":         "session.updated",
	})
	if err != nil {
		return db.SessionEvent{}, err
	}
	return db.SessionEvent{
		UUID:              uuid.NewV4().String(),
		ExternalID:        eventID,
		OrganizationUUID:  session.OrganizationUUID,
		WorkspaceUUID:     session.WorkspaceUUID,
		SessionUUID:       session.UUID,
		SessionExternalID: session.ExternalID,
		EventType:         "session.updated",
		Payload:           payload,
		ProcessedAt:       now,
		CreatedAt:         now,
	}, nil
}

func (h *Handler) simpleSessionEvent(eventType, sessionID string, threadID *string) (db.SessionEvent, error) {
	eventID, err := ids.New("sevt_")
	if err != nil {
		return db.SessionEvent{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	payload := map[string]any{
		"id":           eventID,
		"created_at":   now.Format(time.RFC3339Nano),
		"processed_at": now.Format(time.RFC3339Nano),
		"type":         eventType,
	}
	if threadID != nil {
		payload["session_thread_id"] = *threadID
	}
	raw, err := jsonv2.Marshal(payload)
	if err != nil {
		return db.SessionEvent{}, err
	}
	return db.SessionEvent{
		UUID:             uuid.NewV4().String(),
		ExternalID:       eventID,
		ThreadExternalID: threadID,
		EventType:        eventType,
		Payload:          raw,
		ProcessedAt:      now,
		CreatedAt:        now,
	}, nil
}

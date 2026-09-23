package sessions

import (
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func sessionEventStateChange(event db.SessionEvent) *db.SessionEventStateChange {
	if status, ok := maevents.ThreadStatus(event.EventType); ok {
		if threadID := sessionThreadIDFromEvent(event); threadID != nil {
			return &db.SessionEventStateChange{ThreadExternalID: *threadID, Status: status, AggregateSession: true}
		}
	}
	if status, ok := maevents.SessionStatus(event.EventType); ok {
		return &db.SessionEventStateChange{Status: status}
	}
	return nil
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
	now := time.Now().UTC()
	payload := map[string]any{
		"id":           eventID,
		"created_at":   httpapi.FormatTime(now),
		"processed_at": now.Format(time.RFC3339),
		"type":         eventType,
	}
	if threadID != nil {
		payload["session_thread_id"] = *threadID
	}
	raw, err := httpapi.MarshalRaw(payload)
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

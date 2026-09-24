package sessions

import (
	jsonv2 "encoding/json/v2"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

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
	now := eventTime(time.Now())
	payload := map[string]any{
		"id":           eventID,
		"created_at":   formatEventTime(now),
		"processed_at": formatEventTime(now),
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

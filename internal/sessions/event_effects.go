package sessions

import (
	jsonv2 "encoding/json/v2"
	"reflect"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

func changedSessionFields(before, after db.Session) map[string]any {
	fields := map[string]any{}
	if !reflect.DeepEqual(before.Title, after.Title) {
		fields["title"] = after.Title
	}
	if !agentsnapshot.SameRawJSON(before.Metadata, after.Metadata) {
		fields["metadata"] = agentsnapshot.RawJSONValue(after.Metadata, map[string]any{})
	}
	if !agentsnapshot.SameRawJSON(before.AgentSnapshot, after.AgentSnapshot) {
		fields["agent"] = agentsnapshot.RawJSONValue(after.AgentSnapshot, nil)
	}
	if !agentsnapshot.SameRawJSON(before.Budget, after.Budget) {
		fields["budget"] = agentsnapshot.RawJSONValue(after.Budget, nil)
	}
	return fields
}

func (h *Handler) sessionUpdatedEvent(session db.Session, fields map[string]any) (db.SessionEvent, error) {
	eventID, err := ids.New("sevt_")
	if err != nil {
		return db.SessionEvent{}, err
	}
	now := time.Now().UTC()
	fields["id"] = eventID
	fields["processed_at"] = now.Format(time.RFC3339)
	fields["type"] = "session.updated"
	payload, err := httpapi.MarshalRaw(fields)
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

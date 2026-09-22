package sessions

import (
	"context"
	jsonv2 "encoding/json/v2"
	"reflect"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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
		return append(running, events...), nil
	}
	return events, nil
}

func (h *Handler) sessionUpdatedEvent(previous, session db.Session) (db.SessionEvent, bool, error) {
	eventID, err := ids.New("sevt_")
	if err != nil {
		return db.SessionEvent{}, false, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	fields := make(map[string]any)
	if !reflect.DeepEqual(previous.Title, session.Title) {
		fields["title"] = session.Title
	}
	oldAgent, newAgent := agentsnapshot.RawJSONValue(previous.AgentSnapshot, nil), agentsnapshot.RawJSONValue(session.AgentSnapshot, nil)
	if !reflect.DeepEqual(oldAgent, newAgent) {
		fields["agent"] = newAgent
	}
	oldMetadata, newMetadata := agentsnapshot.RawJSONValue(previous.Metadata, nil), agentsnapshot.RawJSONValue(session.Metadata, nil)
	if !reflect.DeepEqual(oldMetadata, newMetadata) {
		fields["metadata"] = newMetadata
	}
	if len(fields) == 0 {
		return db.SessionEvent{}, false, nil
	}
	fields["id"], fields["type"], fields["processed_at"], fields["created_at"] = eventID, "session.updated", now, now
	payload, err := jsonv2.Marshal(fields)
	if err != nil {
		return db.SessionEvent{}, false, err
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
	}, true, nil
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

func (h *Handler) terminationEvents(ctx context.Context, session db.Session, threadID string) ([]db.SessionEvent, error) {
	threads, err := h.db.ListSessionThreads(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	var events []db.SessionEvent
	for _, thread := range threads {
		if threadID != "" && thread.ExternalID != threadID {
			continue
		}
		event, err := h.simpleSessionEvent("session.thread_status_terminated", session.ExternalID, new(thread.ExternalID))
		if err != nil {
			return nil, err
		}
		mapped, err := h.sessionEventsFromCodeSessionPayload(ctx, session, session.ExternalID, event.Payload, event.CreatedAt)
		if err != nil {
			return nil, err
		}
		events = append(events, mapped...)
	}
	if threadID == "" {
		event, err := h.simpleSessionEvent("session.status_terminated", session.ExternalID, nil)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

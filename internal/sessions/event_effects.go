package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (h *Handler) applySessionEventProjection(ctx context.Context, event db.SessionEvent) error {
	if event.EventType == "session.thread_created" {
		session, found, err := h.db.GetSession(ctx, event.WorkspaceUUID, event.SessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if threadID := sessionThreadIDFromEvent(event); threadID != nil {
			var payload map[string]any
			_ = json.Unmarshal(event.Payload, &payload)
			if err := h.ensureSessionThread(ctx, session, *threadID, payload, event.CreatedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
				return err
			}
		}
		return nil
	}
	if status, ok := threadStatusFromEventType(event.EventType); ok {
		threadID := sessionThreadIDFromEvent(event)
		if threadID == nil {
			return nil
		}
		session, found, err := h.db.GetSession(ctx, event.WorkspaceUUID, event.SessionExternalID)
		if err != nil {
			return err
		}
		if found {
			var payload map[string]any
			_ = json.Unmarshal(event.Payload, &payload)
			if err := h.ensureSessionThread(ctx, session, *threadID, payload, event.CreatedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
				return err
			}
		}
		if err := h.db.SetSessionThreadStatus(ctx, event.WorkspaceUUID, event.SessionExternalID, *threadID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
		return h.projectAggregatedSessionStatus(ctx, event.WorkspaceUUID, event.SessionExternalID)
	}
	status, ok := sessionStatusFromEventType(event.EventType)
	if !ok {
		return nil
	}
	thread, found, err := h.db.GetPrimarySessionThread(ctx, event.WorkspaceUUID, event.SessionExternalID)
	if err != nil {
		return err
	}
	if found {
		if err := h.db.SetSessionThreadStatus(ctx, event.WorkspaceUUID, event.SessionExternalID, thread.ExternalID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
	}
	if err := h.db.SetSessionStatus(ctx, event.WorkspaceUUID, event.SessionExternalID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	return nil
}

func sessionStatusFromEventType(eventType string) (string, bool) {
	return maevents.SessionStatus(eventType)
}

func threadStatusFromEventType(eventType string) (string, bool) {
	return maevents.ThreadStatus(eventType)
}

func (h *Handler) projectAggregatedSessionStatus(ctx context.Context, workspaceUUID string, sessionID string) error {
	threads, err := h.db.ListSessionThreads(ctx, workspaceUUID, sessionID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if len(threads) == 0 {
		return nil
	}
	status := "terminated"
	for _, thread := range threads {
		switch thread.Status {
		case "running":
			status = "running"
			if err := h.db.SetSessionStatus(ctx, workspaceUUID, sessionID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
				return err
			}
			return nil
		case "rescheduling":
			if status != "running" {
				status = "rescheduling"
			}
		case "idle":
			if status != "running" && status != "rescheduling" {
				status = "idle"
			}
		}
	}
	if err := h.db.SetSessionStatus(ctx, workspaceUUID, sessionID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
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

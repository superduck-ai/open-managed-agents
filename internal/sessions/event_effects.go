package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/google/uuid"
)

func (h *Handler) applySessionEventEffects(ctx context.Context, event db.SessionEvent) {
	if event.EventType == "session.thread_created" {
		session, err := h.db.GetSession(ctx, event.WorkspaceID, event.SessionExternalID)
		if err != nil {
			if !errors.Is(err, db.ErrNotFound) {
				log.Printf("load session for thread_created session_id=%s: %v", event.SessionExternalID, err)
			}
			return
		}
		if threadID := sessionThreadIDFromEvent(event); threadID != nil {
			var payload map[string]any
			_ = json.Unmarshal(event.Payload, &payload)
			if err := h.ensureSessionThread(ctx, session, *threadID, payload, event.CreatedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
				log.Printf("ensure session thread session_id=%s thread_id=%s: %v", event.SessionExternalID, *threadID, err)
			}
		}
		return
	}
	if status, ok := threadStatusFromEventType(event.EventType); ok {
		threadID := sessionThreadIDFromEvent(event)
		if threadID == nil {
			return
		}
		session, err := h.db.GetSession(ctx, event.WorkspaceID, event.SessionExternalID)
		if err == nil {
			var payload map[string]any
			_ = json.Unmarshal(event.Payload, &payload)
			if err := h.ensureSessionThread(ctx, session, *threadID, payload, event.CreatedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
				log.Printf("ensure session thread for status session_id=%s thread_id=%s: %v", event.SessionExternalID, *threadID, err)
			}
		} else if !errors.Is(err, db.ErrNotFound) {
			log.Printf("load session for thread status session_id=%s: %v", event.SessionExternalID, err)
		}
		if err := h.db.SetSessionThreadStatus(ctx, event.WorkspaceID, event.SessionExternalID, *threadID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
			log.Printf("update session thread status from event session_id=%s thread_id=%s event_type=%s: %v", event.SessionExternalID, *threadID, event.EventType, err)
			return
		}
		h.updateAggregatedSessionStatus(ctx, event.WorkspaceID, event.SessionExternalID)
		return
	}
	status, ok := sessionStatusFromEventType(event.EventType)
	if !ok {
		return
	}
	if err := h.db.SetSessionStatus(ctx, event.WorkspaceID, event.SessionExternalID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("update session status from event session_id=%s event_type=%s: %v", event.SessionExternalID, event.EventType, err)
	}
	thread, err := h.db.GetPrimarySessionThread(ctx, event.WorkspaceID, event.SessionExternalID)
	if err != nil {
		return
	}
	if err := h.db.SetSessionThreadStatus(ctx, event.WorkspaceID, event.SessionExternalID, thread.ExternalID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("update primary session thread status from event session_id=%s thread_id=%s event_type=%s: %v", event.SessionExternalID, thread.ExternalID, event.EventType, err)
	}
}

func sessionStatusFromEventType(eventType string) (string, bool) {
	return maevents.SessionStatus(eventType)
}

func threadStatusFromEventType(eventType string) (string, bool) {
	return maevents.ThreadStatus(eventType)
}

func (h *Handler) updateAggregatedSessionStatus(ctx context.Context, workspaceID int64, sessionID string) {
	threads, err := h.db.ListSessionThreads(ctx, workspaceID, sessionID)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			log.Printf("list session threads for aggregate session_id=%s: %v", sessionID, err)
		}
		return
	}
	if len(threads) == 0 {
		return
	}
	status := "terminated"
	for _, thread := range threads {
		switch thread.Status {
		case "running":
			status = "running"
			if err := h.db.SetSessionStatus(ctx, workspaceID, sessionID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
				log.Printf("aggregate session status session_id=%s: %v", sessionID, err)
			}
			return
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
	if err := h.db.SetSessionStatus(ctx, workspaceID, sessionID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("aggregate session status session_id=%s: %v", sessionID, err)
	}
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
		UUID:              uuid.NewString(),
		ExternalID:        eventID,
		OrganizationID:    session.OrganizationID,
		WorkspaceID:       session.WorkspaceID,
		SessionID:         session.ID,
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
		UUID:             uuid.NewString(),
		ExternalID:       eventID,
		ThreadExternalID: threadID,
		EventType:        eventType,
		Payload:          raw,
		ProcessedAt:      now,
		CreatedAt:        now,
	}, nil
}

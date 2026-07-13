package sessions

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

func (h *Handler) enqueueWebhooksForSessionEvents(ctx context.Context, workspaceID int64, sessionID string, events []db.SessionEvent) {
	if len(events) == 0 {
		return
	}
	workspaceIDs, err := h.db.GetWorkspaceIdentifiers(ctx, workspaceID)
	if err != nil {
		log.Printf("load workspace identifiers for session webhook session_id=%s: %v", sessionID, err)
		return
	}
	seen := map[string]struct{}{}
	for _, event := range events {
		for _, webhookEvent := range webhookEventsFromSessionEvent(event) {
			key := webhookEvent.EventType + "\x00" + event.CreatedAt.Format(time.RFC3339Nano)
			if webhookEvent.ThreadID != nil {
				key += "\x00" + *webhookEvent.ThreadID
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			webhooks.Enqueue(ctx, h.db, h.cfg, workspaceID, workspaceIDs.OrganizationExternalID, workspaceIDs.WorkspaceExternalID, webhookEvent.EventType, sessionID, webhookEvent.ThreadID)
		}
	}
}

type sessionWebhookEvent struct {
	EventType string
	ThreadID  *string
}

func webhookEventsFromSessionEvent(event db.SessionEvent) []sessionWebhookEvent {
	switch event.EventType {
	case "session.status_run_started", "session.status_running", "session.running":
		return []sessionWebhookEvent{{EventType: "session.status_run_started"}}
	case "session.status_idle", "session.status_idled", "session.idled", "session.requires_action":
		return []sessionWebhookEvent{{EventType: "session.status_idled"}}
	case "session.status_rescheduled":
		return []sessionWebhookEvent{{EventType: "session.status_rescheduled"}}
	case "session.status_terminated":
		return []sessionWebhookEvent{{EventType: "session.status_terminated"}}
	case "session.deleted":
		return []sessionWebhookEvent{{EventType: "session.deleted"}}
	case "session.updated":
		return []sessionWebhookEvent{{EventType: "session.updated"}}
	case "session.error":
		return []sessionWebhookEvent{{EventType: "session.error"}}
	case "session.thread_created":
		return []sessionWebhookEvent{{EventType: "session.thread_created", ThreadID: sessionThreadIDFromEvent(event)}}
	case "session.thread_status_running":
		threadID := sessionThreadIDFromEvent(event)
		return []sessionWebhookEvent{{EventType: "session.thread_status_running", ThreadID: threadID}}
	case "session.thread_status_idle", "session.thread_idled":
		threadID := sessionThreadIDFromEvent(event)
		return []sessionWebhookEvent{
			{EventType: "session.thread_status_idle", ThreadID: threadID},
			{EventType: "session.thread_idled", ThreadID: threadID},
		}
	case "session.thread_status_rescheduled":
		threadID := sessionThreadIDFromEvent(event)
		return []sessionWebhookEvent{{EventType: "session.thread_status_rescheduled", ThreadID: threadID}}
	case "session.thread_status_terminated", "session.thread_terminated":
		threadID := sessionThreadIDFromEvent(event)
		return []sessionWebhookEvent{
			{EventType: "session.thread_status_terminated", ThreadID: threadID},
			{EventType: "session.thread_terminated", ThreadID: threadID},
		}
	case "session.outcome_evaluation_ended":
		return []sessionWebhookEvent{{EventType: "session.outcome_evaluation_ended"}}
	default:
		return nil
	}
}

func sessionThreadIDFromEvent(event db.SessionEvent) *string {
	var payload struct {
		SessionThreadID string `json:"session_thread_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err == nil {
		if value := strings.TrimSpace(payload.SessionThreadID); value != "" {
			return &value
		}
	}
	if event.ThreadExternalID != nil && strings.TrimSpace(*event.ThreadExternalID) != "" {
		value := strings.TrimSpace(*event.ThreadExternalID)
		return &value
	}
	return nil
}

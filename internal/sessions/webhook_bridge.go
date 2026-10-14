package sessions

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

func (h *Handler) enqueueWebhooksForSessionEvents(ctx context.Context, workspaceUUID, sessionID string, events []db.SessionEvent) {
	if h.webhooks == nil || len(events) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, webhooks.EnqueueTimeout)
	defer cancel()
	var workspaceIDs db.WorkspaceIdentifiers
	var workspaceLoaded, primaryLoaded, primaryFound bool
	var primaryID string
	seen := map[string]struct{}{}
	for _, event := range events {
		if ctx.Err() != nil {
			return
		}
		for _, webhookEvent := range webhookEventsFromSessionEvent(event) {
			if strings.HasPrefix(webhookEvent.EventType, "session.thread_") {
				if webhookEvent.ThreadID == nil {
					continue
				}
				if !primaryLoaded {
					var err error
					primaryID, primaryFound, err = h.db.GetPrimarySessionThreadExternalID(ctx, workspaceUUID, sessionID)
					primaryLoaded = true
					if err != nil {
						primaryFound = false
						h.logger.ErrorContext(ctx, "load primary thread for session webhook", "session_id", sessionID, "error", err)
					}
				}
				if !primaryFound || *webhookEvent.ThreadID == primaryID {
					continue
				}
			}
			if !workspaceLoaded {
				var err error
				workspaceIDs, err = h.db.GetWorkspaceIdentifiers(ctx, workspaceUUID)
				if err != nil {
					h.logger.ErrorContext(ctx, "load workspace identifiers for session webhook", "session_id", sessionID, "error", err)
					return
				}
				workspaceLoaded = true
			}
			key := webhookEvent.EventType + "\x00" + event.CreatedAt.Format(time.RFC3339Nano)
			if webhookEvent.ThreadID != nil {
				key += "\x00" + *webhookEvent.ThreadID
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			h.enqueueWebhook(ctx, webhooks.EnqueueInput{
				OccurredAt:          event.CreatedAt,
				WorkspaceUUID:       workspaceUUID,
				OrganizationUUID:    workspaceIDs.OrganizationUUID,
				WorkspaceExternalID: workspaceIDs.WorkspaceExternalID,
				EventType:           webhookEvent.EventType,
				ResourceID:          sessionID,
				Options:             webhooks.EventOptions{SessionThreadID: webhookEvent.ThreadID},
			})
		}
	}
}

func (h *Handler) enqueueWebhook(ctx context.Context, input webhooks.EnqueueInput) {
	if h.webhooks != nil {
		h.webhooks.Enqueue(ctx, input)
	}
}

func (h *Handler) enqueuePrincipalWebhook(ctx context.Context, principal auth.Principal, eventType, resourceID string, sessionThreadID *string, occurredAt time.Time) {
	h.enqueueWebhook(ctx, webhooks.EnqueueInput{
		OccurredAt:          occurredAt,
		WorkspaceUUID:       principal.WorkspaceUUID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceExternalID: principal.WorkspaceExternalID,
		EventType:           eventType,
		ResourceID:          resourceID,
		Options:             webhooks.EventOptions{SessionThreadID: sessionThreadID},
	})
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
	case "session.thread_created":
		return []sessionWebhookEvent{{EventType: "session.thread_created", ThreadID: sessionThreadIDFromEvent(event)}}
	case "session.thread_status_idle", "session.thread_idled":
		return []sessionWebhookEvent{{EventType: "session.thread_idled", ThreadID: sessionThreadIDFromEvent(event)}}
	case "session.thread_status_terminated", "session.thread_terminated":
		return []sessionWebhookEvent{{EventType: "session.thread_terminated", ThreadID: sessionThreadIDFromEvent(event)}}
	case "span.outcome_evaluation_end":
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
		if value := payload.SessionThreadID; value != "" {
			return &value
		}
	}
	if event.ThreadExternalID != nil && *event.ThreadExternalID != "" {
		value := *event.ThreadExternalID
		return &value
	}
	return nil
}

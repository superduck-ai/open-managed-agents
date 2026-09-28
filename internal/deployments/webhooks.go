package deployments

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

type webhookEnqueuer interface {
	Enqueue(context.Context, webhooks.EnqueueInput)
}

// WithWebhooks binds notifications before HTTP handlers or scheduled workers start.
func (s *Store) WithWebhooks(enqueuer webhookEnqueuer) *Store {
	s.webhooks = enqueuer
	return s
}

func (s *Store) enqueueSessionCreated(ctx context.Context, session db.Session) {
	s.enqueueResource(ctx, session.WorkspaceUUID, session.ExternalID, session.CreatedAt,
		"session.status_idled")
}

func (s *Store) enqueueResource(ctx context.Context, workspaceUUID, resourceID string, occurredAt time.Time, eventTypes ...string) {
	if s.webhooks == nil {
		return
	}
	scope, err := s.database.GetWorkspaceIdentifiers(ctx, workspaceUUID)
	if err != nil {
		s.logger.ErrorContext(ctx, "load workspace identifiers for deployment webhook", "resource_id", resourceID, "error", err)
		return
	}
	for _, eventType := range eventTypes {
		s.webhooks.Enqueue(ctx, webhooks.EnqueueInput{
			OccurredAt:          occurredAt,
			WorkspaceUUID:       workspaceUUID,
			OrganizationUUID:    scope.OrganizationUUID,
			WorkspaceExternalID: scope.WorkspaceExternalID,
			EventType:           eventType,
			ResourceID:          resourceID,
		})
	}
}

// Scheduled occurrences persist their Run and final Session-creation result in one
// transaction. Publish both lifecycle signals after commit, with the same Run ID.
func (s *Store) enqueueScheduledRun(ctx context.Context, run db.DeploymentRun, completedAt time.Time) {
	if run.TriggerType != "schedule" || run.ExternalID == "" {
		return
	}
	outcome := "deployment_run.failed"
	if run.SessionExternalID != nil {
		outcome = "deployment_run.succeeded"
	}
	s.enqueueResource(ctx, run.WorkspaceUUID, run.ExternalID, run.CreatedAt, "deployment_run.started")
	s.enqueueResource(ctx, run.WorkspaceUUID, run.ExternalID, completedAt, outcome)
}

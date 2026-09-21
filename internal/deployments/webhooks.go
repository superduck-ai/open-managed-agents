package deployments

import (
	"context"

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
	// The first two events remain available only through legacy global configuration.
	s.enqueueResource(ctx, session.WorkspaceUUID, session.ExternalID,
		"session.created", "session.pending", "session.status_idled")
}

func (s *Store) enqueueResource(ctx context.Context, workspaceUUID, resourceID string, eventTypes ...string) {
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
			WorkspaceUUID:       workspaceUUID,
			OrganizationUUID:    scope.OrganizationUUID,
			WorkspaceExternalID: scope.WorkspaceExternalID,
			EventType:           eventType,
			ResourceID:          resourceID,
		})
	}
}

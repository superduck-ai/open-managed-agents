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
	if s.webhooks == nil {
		return
	}
	scope, err := s.database.GetWorkspaceIdentifiers(ctx, session.WorkspaceUUID)
	if err != nil {
		s.logger.ErrorContext(ctx, "load workspace identifiers for deployment session webhook", "session_id", session.ExternalID, "error", err)
		return
	}
	// The first two events remain available only through legacy global configuration.
	for _, eventType := range []string{"session.created", "session.pending", "session.status_idled"} {
		s.webhooks.Enqueue(ctx, webhooks.EnqueueInput{
			WorkspaceUUID:       session.WorkspaceUUID,
			OrganizationUUID:    scope.OrganizationUUID,
			WorkspaceExternalID: scope.WorkspaceExternalID,
			EventType:           eventType,
			ResourceID:          session.ExternalID,
		})
	}
}

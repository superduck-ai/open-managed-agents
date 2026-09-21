package environments

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

type webhookEnqueuer interface {
	Enqueue(context.Context, webhooks.EnqueueInput)
}

// WithWebhooks binds the shared enqueuer before requests are served.
func (h *Handler) WithWebhooks(enqueuer webhookEnqueuer) *Handler {
	h.webhooks = enqueuer
	return h
}

func (h *Handler) enqueueWebhook(ctx context.Context, principal auth.Principal, eventType, environmentID string) {
	if h.webhooks == nil {
		return
	}
	h.webhooks.Enqueue(ctx, webhooks.EnqueueInput{
		WorkspaceUUID:       principal.WorkspaceUUID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceExternalID: principal.WorkspaceExternalID,
		EventType:           eventType,
		ResourceID:          environmentID,
	})
}

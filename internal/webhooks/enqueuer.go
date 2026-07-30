package webhooks

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

// EnqueueInput contains event-specific values while Enqueuer owns the stable
// database, configuration, and logger dependencies.
type EnqueueInput struct {
	WorkspaceID         int64
	OrganizationUUID    string
	WorkspaceExternalID string
	EventType           string
	ResourceID          string
	Options             EventOptions
}

type enqueueStore interface {
	HasWebhookEndpoints(ctx context.Context, workspaceID int64) (bool, error)
	ListActiveWebhookEndpointsForEvent(ctx context.Context, workspaceID int64, eventType string) ([]db.WebhookEndpoint, error)
	EnqueueWebhookDeliveryJobForEndpoint(ctx context.Context, workspaceID int64, eventType string, event json.RawMessage, endpointID int64) error
	EnqueueWebhookDeliveryJob(ctx context.Context, workspaceID int64, eventType string, event json.RawMessage) error
}

// Enqueuer creates webhook events and persists delivery jobs.
type Enqueuer struct {
	store  enqueueStore
	cfg    config.WebhookConfig
	logger *slog.Logger
}

// NewEnqueuer constructs a webhook event enqueuer with component-owned dependencies.
func NewEnqueuer(database *db.DB, cfg config.WebhookConfig, logger *slog.Logger) *Enqueuer {
	return newEnqueuer(database, cfg, logger)
}

func newEnqueuer(store enqueueStore, cfg config.WebhookConfig, logger *slog.Logger) *Enqueuer {
	return &Enqueuer{
		store:  store,
		cfg:    cfg,
		logger: logging.LoggerOrDefault(logger),
	}
}

// Enqueue creates delivery jobs for one webhook event.
func (e *Enqueuer) Enqueue(ctx context.Context, input EnqueueInput) {
	if e == nil || e.store == nil {
		return
	}
	eventID, err := ids.New("wevt_")
	if err != nil {
		e.logger.ErrorContext(ctx, "webhook event id", "error", err)
		return
	}
	event := Event{
		ID:        eventID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Data: EventData{
			ID:              input.ResourceID,
			OrganizationID:  input.OrganizationUUID,
			Type:            input.EventType,
			WorkspaceID:     input.WorkspaceExternalID,
			SessionThreadID: input.Options.SessionThreadID,
			VaultID:         input.Options.VaultID,
		},
		Type: "event",
	}
	payload, err := json.Marshal(event)
	if err != nil {
		e.logger.ErrorContext(ctx, "marshal webhook event", "event_type", input.EventType, "resource_id", input.ResourceID, "error", err)
		return
	}

	hasEndpoints, err := e.store.HasWebhookEndpoints(ctx, input.WorkspaceID)
	if err != nil {
		e.logger.ErrorContext(ctx, "load webhook endpoint configuration", "workspace_id", input.WorkspaceID, "error", err)
		return
	}
	if hasEndpoints {
		e.enqueueForEndpoints(ctx, input, payload)
		return
	}

	if !enabled(e.cfg) || !subscribed(e.cfg, input.EventType) {
		return
	}
	if err := e.store.EnqueueWebhookDeliveryJob(ctx, input.WorkspaceID, input.EventType, payload); err != nil {
		e.logger.ErrorContext(ctx, "enqueue webhook event", "event_type", input.EventType, "resource_id", input.ResourceID, "error", err)
	}
}

func (e *Enqueuer) enqueueForEndpoints(ctx context.Context, input EnqueueInput, payload json.RawMessage) {
	endpoints, err := e.store.ListActiveWebhookEndpointsForEvent(ctx, input.WorkspaceID, input.EventType)
	if err != nil {
		e.logger.ErrorContext(ctx, "list webhook endpoints event", "event_type", input.EventType, "workspace_id", input.WorkspaceID, "error", err)
		return
	}
	for _, endpoint := range endpoints {
		if err := e.store.EnqueueWebhookDeliveryJobForEndpoint(ctx, input.WorkspaceID, input.EventType, payload, endpoint.ID); err != nil {
			e.logger.ErrorContext(ctx, "enqueue webhook event", "endpoint_id", endpoint.ExternalID, "event_type", input.EventType, "resource_id", input.ResourceID, "error", err)
		}
	}
}

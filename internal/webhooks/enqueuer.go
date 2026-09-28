package webhooks

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

// EnqueueInput contains event-specific values while Enqueuer owns the stable
// database and logger dependencies.
type EnqueueInput struct {
	OccurredAt          time.Time
	WorkspaceUUID       string
	OrganizationUUID    string
	WorkspaceExternalID string
	EventType           string
	ResourceID          string
	Options             EventOptions
}

type enqueueStore interface {
	ListActiveWebhookEndpointsForEvent(ctx context.Context, workspaceUUID, eventType string) ([]db.WebhookEndpoint, error)
	EnqueueWebhookDeliveryJobForEndpoint(ctx context.Context, workspaceUUID, eventType string, event json.RawMessage, endpointUUID string) error
}

// Enqueuer creates webhook events and persists delivery jobs.
type Enqueuer struct {
	store  enqueueStore
	logger *slog.Logger
}

// NewEnqueuer constructs a webhook event enqueuer with component-owned dependencies.
func NewEnqueuer(database *db.DB, logger *slog.Logger) *Enqueuer {
	return newEnqueuer(database, logger)
}

func newEnqueuer(store enqueueStore, logger *slog.Logger) *Enqueuer {
	return &Enqueuer{
		store:  store,
		logger: logging.LoggerOrDefault(logger),
	}
}

// Enqueue creates delivery jobs for one webhook event.
func (e *Enqueuer) Enqueue(ctx context.Context, input EnqueueInput) {
	if e == nil || e.store == nil {
		return
	}
	if input.OccurredAt.IsZero() {
		e.logger.ErrorContext(ctx, "webhook occurrence time missing", "event_type", input.EventType, "resource_id", input.ResourceID)
		return
	}
	endpoints, err := e.store.ListActiveWebhookEndpointsForEvent(ctx, input.WorkspaceUUID, input.EventType)
	if err != nil {
		e.logger.ErrorContext(ctx, "list webhook endpoints event", "event_type", input.EventType, "workspace_uuid", input.WorkspaceUUID, "error", err)
		return
	}
	if len(endpoints) == 0 {
		return
	}
	eventID, err := ids.New("wevt_")
	if err != nil {
		e.logger.ErrorContext(ctx, "webhook event id", "error", err)
		return
	}
	event := Event{
		ID:        eventID,
		CreatedAt: input.OccurredAt.UTC().Format(time.RFC3339Nano),
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

	for _, endpoint := range endpoints {
		if err := e.store.EnqueueWebhookDeliveryJobForEndpoint(ctx, input.WorkspaceUUID, input.EventType, payload, endpoint.UUID); err != nil {
			e.logger.ErrorContext(ctx, "enqueue webhook event", "endpoint_uuid", endpoint.ExternalID, "event_type", input.EventType, "resource_id", input.ResourceID, "error", err)
		}
	}
}

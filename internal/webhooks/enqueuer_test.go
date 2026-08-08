package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPrepareDeliveryEventPreservesOutboxData(t *testing.T) {
	createdAt := time.Date(2026, time.August, 7, 1, 2, 3, 0, time.UTC)
	enqueuer := newEnqueuer(failingEnqueueStore{}, config.WebhookConfig{
		WorkerEnabled: true, EndpointURL: "https://example.test", SigningKey: "secret",
		EventTypes: []string{"deployment_run.started"},
	}, nil)
	deliveryEvent, err := enqueuer.PrepareDeliveryEvent(EnqueueInput{
		OrganizationUUID: "org-uuid", WorkspaceExternalID: "workspace_test",
		EventType: "deployment_run.started", ResourceID: "drun_test",
	}, createdAt)
	if err != nil {
		t.Fatalf("PrepareDeliveryEvent() error = %v", err)
	}
	var event Event
	if err := json.Unmarshal(deliveryEvent.Event, &event); err != nil {
		t.Fatalf("decode prepared event: %v", err)
	}
	if !deliveryEvent.FallbackEnabled || deliveryEvent.EventType != "deployment_run.started" ||
		event.CreatedAt != "2026-08-07T01:02:03Z" || event.Data.ID != "drun_test" {
		t.Fatalf("PrepareDeliveryEvent() = %+v, event = %+v", deliveryEvent, event)
	}
}

type failingEnqueueStore struct{}

func (failingEnqueueStore) HasWebhookEndpoints(context.Context, string) (bool, error) {
	return false, errors.New("load endpoints")
}

func (failingEnqueueStore) ListActiveWebhookEndpointsForEvent(context.Context, string, string) ([]db.WebhookEndpoint, error) {
	return nil, nil
}

func (failingEnqueueStore) EnqueueWebhookDeliveryJobForEndpoint(context.Context, string, string, json.RawMessage, string) error {
	return nil
}

func (failingEnqueueStore) EnqueueWebhookDeliveryJob(context.Context, string, string, json.RawMessage) error {
	return nil
}

func TestEnqueuerUsesOwnedLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With("component", "webhooks")
	enqueuer := newEnqueuer(failingEnqueueStore{}, config.WebhookConfig{}, logger)

	enqueuer.Enqueue(context.Background(), EnqueueInput{
		WorkspaceUUID:       "00000000-0000-0000-0000-000000000042",
		OrganizationUUID:    "11111111-1111-4111-8111-111111111111",
		WorkspaceExternalID: "wrk_test",
		EventType:           "session.created",
		ResourceID:          "session_test",
	})

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log record: %v", err)
	}
	if got := record["component"]; got != "webhooks" {
		t.Fatalf("component = %v, want webhooks", got)
	}
	if got := record["msg"]; got != "load webhook endpoint configuration" {
		t.Fatalf("msg = %v, want load webhook endpoint configuration", got)
	}
	if got := record["workspace_uuid"]; got != "00000000-0000-0000-0000-000000000042" {
		t.Fatalf("workspace_uuid = %v, want UUID", got)
	}
}

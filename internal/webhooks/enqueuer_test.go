package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

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

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
		OccurredAt:          time.Now().UTC(),
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

type capturingEnqueueStore struct {
	failingEnqueueStore
	payloads []json.RawMessage
}

func (s *capturingEnqueueStore) HasWebhookEndpoints(context.Context, string) (bool, error) {
	return true, nil
}
func (s *capturingEnqueueStore) ListActiveWebhookEndpointsForEvent(context.Context, string, string) ([]db.WebhookEndpoint, error) {
	return []db.WebhookEndpoint{{UUID: "one"}, {UUID: "two"}}, nil
}
func (s *capturingEnqueueStore) EnqueueWebhookDeliveryJobForEndpoint(_ context.Context, _, _ string, event json.RawMessage, _ string) error {
	s.payloads = append(s.payloads, append(json.RawMessage(nil), event...))
	return nil
}

func TestEnqueuerOccurrenceTime(t *testing.T) {
	store := &capturingEnqueueStore{}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	enqueuer := newEnqueuer(store, config.WebhookConfig{}, logger)
	input := EnqueueInput{EventType: "agent.created", ResourceID: "agent_test"}
	enqueuer.Enqueue(t.Context(), input)
	if len(store.payloads) != 0 {
		t.Fatal("zero time created jobs")
	}
	var record struct {
		Level      string `json:"level"`
		Message    string `json:"msg"`
		EventType  string `json:"event_type"`
		ResourceID string `json:"resource_id"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Level != "ERROR" || record.Message != "webhook occurrence time missing" || record.EventType != input.EventType || record.ResourceID != input.ResourceID {
		t.Fatalf("log = %+v", record)
	}
	input.OccurredAt = time.Date(2020, 1, 2, 3, 4, 5, 123456789, time.FixedZone("source", 8*3600))
	enqueuer.Enqueue(t.Context(), input)
	if len(store.payloads) != 2 || !bytes.Equal(store.payloads[0], store.payloads[1]) {
		t.Fatal("fanout changed payload")
	}
	var event Event
	if err := json.Unmarshal(store.payloads[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.CreatedAt != "2020-01-01T19:04:05.123456789Z" || event.ID == "" {
		t.Fatalf("event = %+v", event)
	}
}

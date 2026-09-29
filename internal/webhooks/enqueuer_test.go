package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"
)

type failingEnqueueStore struct{}

func (failingEnqueueStore) ListActiveWebhookEndpointUUIDs(context.Context, string, string) ([]string, error) {
	return nil, errors.New("load endpoints")
}

func (failingEnqueueStore) Publish(context.Context, Envelope) error {
	return nil
}

func TestEnqueuerUsesOwnedLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With("component", "webhooks")
	enqueuer := newEnqueuer(failingEnqueueStore{}, failingEnqueueStore{}, logger)

	enqueuer.Enqueue(context.Background(), EnqueueInput{
		OccurredAt:          time.Now().UTC(),
		WorkspaceUUID:       "00000000-0000-0000-0000-000000000042",
		OrganizationUUID:    "11111111-1111-4111-8111-111111111111",
		WorkspaceExternalID: "wrk_test",
		EventType:           "session.status_idled",
		ResourceID:          "session_test",
	})

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log record: %v", err)
	}
	if got := record["component"]; got != "webhooks" {
		t.Fatalf("component = %v, want webhooks", got)
	}
	if got := record["msg"]; got != "list webhook endpoints event" {
		t.Fatalf("msg = %v, want list webhook endpoints event", got)
	}
	if got := record["workspace_uuid"]; got != "00000000-0000-0000-0000-000000000042" {
		t.Fatalf("workspace_uuid = %v, want UUID", got)
	}
}

type capturingEnqueueStore struct {
	payloads []json.RawMessage
}

func (s *capturingEnqueueStore) ListActiveWebhookEndpointUUIDs(context.Context, string, string) ([]string, error) {
	return []string{"one", "two"}, nil
}
func (s *capturingEnqueueStore) Publish(_ context.Context, envelope Envelope) error {
	s.payloads = append(s.payloads, append(json.RawMessage(nil), envelope.Event...))
	return nil
}

func TestEnqueuerOccurrenceTime(t *testing.T) {
	store := &capturingEnqueueStore{}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	enqueuer := newEnqueuer(store, store, logger)
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

type partialPublisher struct {
	calls     int
	fail      bool
	deadlines []time.Duration
}

func (p *partialPublisher) Publish(ctx context.Context, _ Envelope) error {
	p.calls++
	deadline, ok := ctx.Deadline()
	if ok {
		p.deadlines = append(p.deadlines, time.Until(deadline))
	}
	if p.fail && p.calls == 1 {
		return errors.New("queue unavailable")
	}
	return nil
}
func TestEnqueuerPartialFailureAndDeadline(t *testing.T) {
	store := &capturingEnqueueStore{}
	publisher := &partialPublisher{fail: true}
	var logs bytes.Buffer
	enqueuer := newEnqueuer(store, publisher, slog.New(slog.NewJSONHandler(&logs, nil)))
	input := EnqueueInput{OccurredAt: time.Now(), EventType: "agent.created", ResourceID: "agent_test"}
	enqueuer.Enqueue(t.Context(), input)
	if publisher.calls != 2 || len(publisher.deadlines) != 2 {
		t.Fatalf("partial fanout calls=%d", publisher.calls)
	}
	for _, left := range publisher.deadlines {
		if left <= 0 || left > 5*time.Second {
			t.Fatalf("deadline=%v", left)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	enqueuer.Enqueue(ctx, input)
	if publisher.calls != 2 {
		t.Fatal("canceled publisher called")
	}
}

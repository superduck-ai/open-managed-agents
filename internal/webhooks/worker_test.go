package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type resultStore struct {
	target              db.WebhookDeliveryTarget
	lookupErr, writeErr error
	disable             bool
	successes, failures int
}

func (s *resultStore) FindWebhookDeliveryTarget(context.Context, string, string) (db.WebhookDeliveryTarget, bool, error) {
	return s.target, true, s.lookupErr
}
func (s *resultStore) RecordWebhookDeliverySuccess(context.Context, string, string) error {
	s.successes++
	return s.writeErr
}
func (s *resultStore) RecordWebhookDeliveryFailure(context.Context, string, string, string, bool, time.Duration) (bool, error) {
	s.failures++
	return s.disable, s.writeErr
}

type testMessage struct {
	jetstream.Msg
	body            []byte
	deliveries      uint64
	ackErr          error
	acknowledgments int
	delay           time.Duration
}

func (m *testMessage) Data() []byte { return m.body }
func (m *testMessage) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: m.deliveries}, nil
}
func (m *testMessage) DoubleAck(context.Context) error        { m.acknowledgments++; return m.ackErr }
func (m *testMessage) NakWithDelay(delay time.Duration) error { m.delay = delay; return nil }

func testEnvelope(t *testing.T) Envelope {
	t.Helper()
	event, err := json.Marshal(Event{ID: "wevt_" + uuid.NewV4().String(), Type: "event", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Data: EventData{Type: "agent.created", ID: "agent_test"}})
	if err != nil {
		t.Fatal(err)
	}
	return Envelope{Version: 1, WorkspaceUUID: uuid.NewV4().String(), EndpointUUID: uuid.NewV4().String(), Event: event}
}

func TestWebhookWorkerFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		status                         int
		lookupErr, writeErr, ackErr    bool
		disabled                       bool
		attempts, deliveries           int
		wantAck, wantHTTP, wantFailure int
	}{
		{name: "lookup failure", lookupErr: true, deliveries: 1},
		{name: "lookup exhausted", lookupErr: true, deliveries: 3, wantAck: 1},
		{name: "statistics fail after success", status: 204, writeErr: true, deliveries: 1, wantAck: 1, wantHTTP: 1},
		{name: "statistics fail after redirect", status: 302, writeErr: true, deliveries: 1, wantAck: 1, wantHTTP: 1, wantFailure: 1},
		{name: "ack lost does not send inline", status: 204, ackErr: true, deliveries: 1, wantAck: 1, wantHTTP: 1},
		{name: "sustained disable ends current message", status: 500, disabled: true, deliveries: 1, wantAck: 1, wantHTTP: 1, wantFailure: 1},
		{name: "retry", status: 500, deliveries: 1, wantHTTP: 1, wantFailure: 1},
		{name: "default exhaustion", status: 500, deliveries: 3, wantAck: 1, wantHTTP: 1, wantFailure: 1},
		{name: "custom one", status: 500, attempts: 1, deliveries: 1, wantAck: 1, wantHTTP: 1, wantFailure: 1},
		{name: "custom ten", status: 500, attempts: 10, deliveries: 9, wantHTTP: 1, wantFailure: 1},
		{name: "custom ten exhausted", status: 500, attempts: 10, deliveries: 10, wantAck: 1, wantHTTP: 1, wantFailure: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(tc.status) }))
			defer receiver.Close()
			store := &resultStore{target: db.WebhookDeliveryTarget{URL: receiver.URL, Status: "enabled", SigningSecret: "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}, disable: tc.disabled}
			if tc.lookupErr {
				store.lookupErr = errors.New("database unavailable")
			}
			if tc.writeErr {
				store.writeErr = errors.New("database unavailable")
			}
			worker := &Worker{database: store, cfg: config.WebhookConfig{AllowInsecure: true, MaxAttempts: tc.attempts}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			body, err := json.Marshal(testEnvelope(t))
			if err != nil {
				t.Fatal(err)
			}
			message := &testMessage{body: body, deliveries: uint64(tc.deliveries)}
			if tc.ackErr {
				message.ackErr = errors.New("ack lost")
			}
			worker.processMessage(t.Context(), receiver.Client(), message)
			if message.acknowledgments != tc.wantAck || int(calls.Load()) != tc.wantHTTP || store.failures != tc.wantFailure {
				t.Fatalf("ack=%d HTTP=%d failures=%d", message.acknowledgments, calls.Load(), store.failures)
			}
			if tc.wantAck == 0 {
				upper := min(120*time.Second, 5*time.Second<<min(tc.deliveries, 5))
				if message.delay < 5*time.Second || message.delay >= upper {
					t.Fatalf("delay=%v", message.delay)
				}
			} else if message.delay != 0 {
				t.Fatal("terminal message scheduled")
			}
		})
	}
}

func TestWebhookWorkerMalformedMessages(t *testing.T) {
	for _, body := range [][]byte{[]byte("bad json"), []byte(`{"version":2}`), []byte(`{"version":1,"workspace_uuid":"invalid"}`)} {
		store := &resultStore{}
		worker := &Worker{database: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		msg := &testMessage{body: body}
		worker.processMessage(t.Context(), http.DefaultClient, msg)
		if msg.acknowledgments != 1 || store.successes+store.failures != 0 {
			t.Fatal("invalid message not retired")
		}
	}
}

func TestWebhookDeliveryDoesNotWaitForResponseBody(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer receiver.Close()
	envelope := testEnvelope(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	start := time.Now()
	err := deliver(ctx, receiver.Client(), deliveryTarget{URL: receiver.URL, SigningKey: "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}, envelope.Event)
	if err != nil || time.Since(start) > time.Second {
		t.Fatalf("body blocked delivery: %v", err)
	}
}

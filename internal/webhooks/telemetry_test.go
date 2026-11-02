package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestWebhookTracePublication(t *testing.T) {
	provider := sdktrace.NewTracerProvider()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	queue := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 1 << 20, MaxAge: time.Hour, Replicas: 1}, config.WebhookConfig{})
	ctx, span := otel.Tracer("test").Start(t.Context(), "producer")
	defer span.End()
	envelope := testEnvelope(t)
	invalid := envelope
	invalid.Version = 2
	if err := queue.Publish(ctx, invalid); err == nil {
		t.Fatal("invalid envelope accepted")
	}
	if err := queue.Publish(ctx, envelope); err != nil {
		t.Fatal(err)
	}
	message, err := queueStream(t, queue).GetMsg(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(message.Data, body) || message.Header.Get(telemetry.TraceParentHeader) != telemetry.TraceParent(ctx) {
		t.Fatal("publication payload or trace header changed")
	}
}

func TestWebhookDeliveryTelemetry(t *testing.T) {
	reader := telemetrytest.MetricReader(t)
	recorder := telemetrytest.SpanRecorder(t)
	counters := map[string]int64{}
	durations := map[string]uint64{}
	parent := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	for _, tc := range []struct {
		name, outcome                                           string
		status                                                  int
		attempts                                                uint64
		invalid, metadataError, lookupError, disabled, measured bool
		ack                                                     int
	}{
		{name: "invalid envelope", invalid: true, ack: 1},
		{name: "metadata failure", metadataError: true},
		{name: "lookup retry", lookupError: true, attempts: 1, outcome: "retry"},
		{name: "lookup exhausted", lookupError: true, attempts: 3, outcome: "dropped", ack: 1},
		{name: "disabled target", disabled: true, outcome: "dropped", ack: 1},
		{name: "delivery retry", status: 500, attempts: 1, outcome: "retry", measured: true},
		{name: "delivery exhausted", status: 500, attempts: 3, outcome: "dropped", measured: true, ack: 1},
		{name: "delivery success", status: 204, attempts: 1, outcome: "ok", measured: true, ack: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(recorder.Ended())
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			defer receiver.Close()
			store := &resultStore{target: db.WebhookDeliveryTarget{URL: receiver.URL, Status: "enabled", SigningSecret: "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}}
			if tc.lookupError {
				store.lookupErr = errors.New("private lookup detail")
			}
			if tc.disabled {
				store.target.Status = "disabled"
			}
			envelope := testEnvelope(t)
			body, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if tc.invalid {
				body = []byte("invalid")
			}
			message := &testMessage{body: body, deliveries: tc.attempts, headers: nats.Header{telemetry.TraceParentHeader: []string{parent}}}
			if tc.metadataError {
				message.metadataErr = errors.New("private metadata detail")
			}
			worker := &Worker{database: store, cfg: config.WebhookConfig{AllowInsecure: true, MaxAttempts: 3}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			worker.processMessage(t.Context(), receiver.Client(), message)
			if message.acknowledgments != tc.ack {
				t.Fatalf("ack=%d want %d", message.acknowledgments, tc.ack)
			}
			spans := recorder.Ended()[before:]
			if tc.invalid {
				if len(spans) != 0 {
					t.Fatal("invalid envelope traced")
				}
			} else {
				if len(spans) != 1 || spans[0].Name() != "oma.webhook.deliver" || spans[0].Parent().IsValid() || len(spans[0].Links()) != 1 || len(spans[0].Events()) != 0 {
					t.Fatalf("delivery spans: %v", spans)
				}
				for _, a := range spans[0].Attributes() {
					if a.Key != "oma.workspace.uuid" && a.Key != "error.type" {
						t.Fatalf("unexpected span attribute: %v", a)
					}
					if strings.Contains(a.Value.Emit(), "private") {
						t.Fatal("private error leaked")
					}
				}
			}
			if tc.outcome != "" {
				counters[tc.outcome]++
			}
			if tc.measured {
				durations[tc.outcome]++
			}
			data := telemetrytest.Collect(t, reader)
			gotCounters := map[string]int64{}
			gotDurations := map[string]uint64{}
			if len(counters) > 0 {
				for _, point := range telemetrytest.Points(t, data, "oma.webhook.deliveries") {
					outcome, _ := point.Attributes.Value(attribute.Key("oma.outcome"))
					if point.Attributes.Len() != 1 {
						t.Fatal("delivery metric identity leaked")
					}
					gotCounters[outcome.AsString()] = int64(point.Value)
				}
			}
			if len(durations) > 0 {
				for _, point := range telemetrytest.Points(t, data, "oma.webhook.delivery.duration") {
					outcome, _ := point.Attributes.Value(attribute.Key("oma.outcome"))
					gotDurations[outcome.AsString()] = point.Count
					if point.Attributes.Len() != 1 || point.Value <= 0 {
						t.Fatalf("duration: %+v", point)
					}
				}
			}
			if len(gotCounters) != len(counters) || len(gotDurations) != len(durations) {
				t.Fatalf("metric sets: %v, %v", gotCounters, gotDurations)
			}
			for outcome, want := range counters {
				if gotCounters[outcome] != want {
					t.Fatalf("%s counter=%d want %d", outcome, gotCounters[outcome], want)
				}
			}
			for outcome, want := range durations {
				if gotDurations[outcome] != want {
					t.Fatalf("%s duration count=%d want %d", outcome, gotDurations[outcome], want)
				}
			}
		})
	}
}

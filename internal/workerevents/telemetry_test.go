package workerevents

import (
	"context"
	"encoding/json"
	server "github.com/nats-io/nats-server/v2/server"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"testing"
	"time"
)

func TestTelemetryAckRefCompatibility(t *testing.T) {
	var old AckRef
	if err := json.Unmarshal([]byte(`{"ack_subject":"old","public_event_id":"evt_old"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Lane != "" || !old.PublishedAt.IsZero() || old.TraceParent != "" {
		t.Fatalf("old ACK metadata: %+v", old)
	}
	want := AckRef{AckSubject: "ack", Lane: "reply", PublishedAt: time.Now().UTC(), TraceParent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got AckRef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestLaneForEventName(t *testing.T) {
	for _, tc := range []struct{ eventType, subtype, want string }{
		{"unknown", "", "task"}, {"control_request", "initialize", "task"}, {"user", "message", "task"}, {"control_response", "", "reply"}, {"control_request", "interrupt", "reply"},
	} {
		if got := laneForEvent(EnvelopeV1{EventType: tc.eventType, EventSubtype: tc.subtype}).name(); got != tc.want {
			t.Errorf("laneForEvent(%s,%s) = %s, want %s", tc.eventType, tc.subtype, got, tc.want)
		}
	}
}

func TestJetStreamTelemetryPropagation(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	broker, err := NewJetStream(t.Context(), connectNATS(t, srv.ClientURL()), config.WorkerEventStreamConfig{ConsumerInactiveThreshold: time.Minute, MaxBytes: 1 << 20, MaxMsgSize: 1024, Replicas: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(t.Context(), "invalid", EnvelopeV1{CodeSessionID: "invalid id"}); err == nil {
		t.Fatal("invalid subject accepted")
	}
	subscription, err := broker.Subscribe(t.Context(), "cse_telemetry")
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	ctx, parent := provider.Tracer("test").Start(t.Context(), "parent")
	defer parent.End()
	envelope := EventEnvelope("cse_telemetry", "evt_telemetry", "", "control_response", "", []byte(`{"private":"not an attribute"}`), time.Now().Add(time.Hour))
	if err := broker.Publish(ctx, "valid", envelope); err != nil {
		t.Fatal(err)
	}
	delivery := receiveWorkerDelivery(t, subscription)
	if delivery.Lane != "reply" || delivery.PublishedAt.IsZero() || delivery.DeliveryCount != 1 {
		t.Fatalf("delivery metadata: %+v", delivery)
	}
	link, ok := telemetry.LinkFromTraceParent(delivery.TraceParent)
	if !ok {
		t.Fatal("traceparent missing or invalid")
	}
	spans := recorder.Ended()
	if len(spans) != 2 || spans[1].Name() != "oma.worker_event.publish" || spans[1].Parent().SpanID() != parent.SpanContext().SpanID() || spans[1].SpanContext().SpanID() != link.SpanContext.SpanID() {
		t.Fatalf("publish trace: %v", spans)
	}
	if len(spans[1].Attributes()) != 4 || len(spans[1].Events()) != 0 {
		t.Fatalf("unexpected telemetry data: %v", spans[1].Attributes())
	}
}

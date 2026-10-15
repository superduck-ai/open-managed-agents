package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestP2MetricsIgnoreUnmeasuredDurations(t *testing.T) {
	reader := metricReader(t)
	ctx := t.Context()
	RecordWorkerEventDelivered(ctx, "", 2, time.Now().Add(-time.Second))
	RecordWorkerEventDelivered(ctx, "task", 1, time.Time{})
	RecordWorkerEventDelivered(ctx, "task", 1, time.Now().Add(time.Hour))
	RecordWorkerEventProcessed(ctx, "reply", -time.Second)
	RecordFanoutLag(ctx, -time.Second)
	if data := collectedMetrics(t, reader); len(data) != 0 {
		t.Fatalf("invalid observations recorded: %v", data)
	}
	RecordWorkerRegister(ctx, "error")
	RecordToolPermissionResponse(ctx, "deny", "auto-deny", time.Now().Add(-time.Second))
	data := collectedMetrics(t, reader)
	if len(data) != 2 {
		t.Fatalf("unmeasured durations recorded: %v", data)
	}
}

func TestP2MetricNamesAttributesAndValues(t *testing.T) {
	cases := []struct {
		name      string
		record    func(context.Context)
		attrs     map[string]string
		value     float64
		histogram bool
	}{
		{"oma.session.created", func(ctx context.Context) { RecordSessionCreated(ctx, "api") }, map[string]string{"oma.source": "api"}, 1, false},
		{"oma.session.events.accepted", func(ctx context.Context) { RecordSessionEventsAccepted(ctx, "user.message") }, map[string]string{"oma.event.type": "user.message"}, 1, false},
		{"oma.session.events.rejected", func(ctx context.Context) { RecordSessionEventsRejected(ctx, "conflict") }, map[string]string{"oma.reason": "conflict"}, 1, false},
		{"oma.session.handoff.failures", func(ctx context.Context) { RecordSessionHandoffFailure(ctx, "other") }, map[string]string{"oma.reason": "other"}, 1, false},
		{"oma.worker_event.publish.duration", func(ctx context.Context) { RecordWorkerEventPublish(ctx, "reply", "timeout", 2*time.Second) }, map[string]string{"oma.worker_event.lane": "reply", "oma.outcome": "timeout"}, 2, true},
		{"oma.worker_event.queue_wait", func(ctx context.Context) { RecordWorkerEventDelivered(ctx, "task", 2, time.Now().Add(-2*time.Second)) }, map[string]string{"oma.worker_event.lane": "task", "oma.redelivered": "true"}, 2, true},
		{"oma.worker_event.redeliveries", func(ctx context.Context) { RecordWorkerEventDelivered(ctx, "reply", 2, time.Time{}) }, map[string]string{"oma.worker_event.lane": "reply"}, 1, false},
		{"oma.worker_event.processed.latency", func(ctx context.Context) { RecordWorkerEventProcessed(ctx, "task", 2*time.Second) }, map[string]string{"oma.worker_event.lane": "task"}, 2, true},
		{"oma.worker_event.updates", func(ctx context.Context) { RecordWorkerEventUpdate(ctx, "processing", "error") }, map[string]string{"oma.delivery.status": "processing", "oma.outcome": "error"}, 1, false},
		{"oma.sandbox.resume.duration", func(ctx context.Context) { RecordSandboxResume(ctx, "extended", 2*time.Second) }, map[string]string{"oma.outcome": "extended"}, 2, true},
		{"oma.worker.register", func(ctx context.Context) { RecordWorkerRegister(ctx, "ok") }, map[string]string{"oma.outcome": "ok"}, 1, false},
		{"oma.worker.register.delay", func(ctx context.Context) { RecordWorkerRegisterDelay(ctx, 2*time.Second) }, nil, 2, true},
		{"oma.worker.heartbeat.rejected", func(ctx context.Context) { RecordWorkerHeartbeatRejected(ctx, "lease_expired") }, map[string]string{"oma.reason": "lease_expired"}, 1, false},
		{"oma.tool_permission.decisions", func(ctx context.Context) { RecordToolPermissionDecision(ctx, "ask", "mcp") }, map[string]string{"oma.decision": "ask", "oma.tool.kind": "mcp"}, 1, false},
		{"oma.tool_permission.responses", func(ctx context.Context) {
			RecordToolPermissionResponse(ctx, "allow", "tool-confirmation", time.Time{})
		}, map[string]string{"oma.behavior": "allow", "oma.source": "tool-confirmation"}, 1, false},
		{"oma.tool_permission.wait", func(ctx context.Context) {
			RecordToolPermissionResponse(ctx, "allow", "custom-tool-result", time.Now().Add(-2*time.Second))
		}, map[string]string{"oma.source": "custom-tool-result"}, 2, true},
		{"oma.sse.streams", func(ctx context.Context) { AddSSEStreams(ctx, "worker", 1) }, map[string]string{"oma.stream.kind": "worker"}, 1, false},
		{"oma.sse.stream.duration", func(ctx context.Context) {
			AddSSEStreams(ctx, "public", 1)
			CloseSSEStream(ctx, "public", time.Now().Add(-2*time.Second))
		}, map[string]string{"oma.stream.kind": "public"}, 2, true},
		{"oma.sse.subscribers.dropped", RecordSSESubscriberDropped, nil, 1, false},
		{"oma.fanout.publish.failures", RecordFanoutPublishFailure, nil, 1, false},
		{"oma.fanout.lag", func(ctx context.Context) { RecordFanoutLag(ctx, 2*time.Second) }, nil, 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := metricReader(t)
			tc.record(t.Context())
			data := collectedMetrics(t, reader)
			var attrs attribute.Set
			if tc.histogram {
				histogram, ok := data[tc.name].(metricdata.Histogram[float64])
				if !ok || len(histogram.DataPoints) != 1 {
					t.Fatalf("unexpected histogram: %v", data)
				}
				point := histogram.DataPoints[0]
				if point.Count != 1 || point.Sum < tc.value || point.Sum > tc.value+0.5 {
					t.Fatalf("unexpected point: %+v", point)
				}
				attrs = point.Attributes
			} else {
				sum, ok := data[tc.name].(metricdata.Sum[int64])
				if !ok || len(sum.DataPoints) != 1 || float64(sum.DataPoints[0].Value) != tc.value {
					t.Fatalf("unexpected sum: %v", data)
				}
				attrs = sum.DataPoints[0].Attributes
			}
			if attrs.Len() != len(tc.attrs) {
				t.Fatalf("unexpected attributes: %v", attrs)
			}
			for key, want := range tc.attrs {
				got, ok := attrs.Value(attribute.Key(key))
				if !ok || got.Emit() != want {
					t.Fatalf("attribute %s = %v, want %s", key, got, want)
				}
			}
			if tc.name == "oma.sse.stream.duration" {
				if data["oma.sse.streams"].(metricdata.Sum[int64]).DataPoints[0].Value != 0 {
					t.Fatal("stream count did not return to zero")
				}
			}
		})
	}
}

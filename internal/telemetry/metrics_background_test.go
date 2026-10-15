package telemetry

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestBackgroundMetricsOmitUnmeasuredDurations(t *testing.T) {
	reader := metricReader(t)
	RecordRiverJob(t.Context(), "sandbox_lifecycle_sweep", "sandbox_lifecycle", "retry", time.Second, 0)
	RecordWebhookDelivery(t.Context(), "dropped", 0)
	data := collectedMetrics(t, reader)
	if _, ok := data["oma.river.job.queue_wait"]; ok {
		t.Fatal("unmeasured queue wait recorded")
	}
	if _, ok := data["oma.webhook.delivery.duration"]; ok {
		t.Fatal("undelivered webhook duration recorded")
	}
	if len(data) != 3 {
		t.Fatalf("counters/duration: %v", data)
	}
}

func TestBackgroundMetricNamesAndAttributes(t *testing.T) {
	reader := metricReader(t)
	RecordRiverJob(t.Context(), "sandbox_lifecycle_sweep", "sandbox_lifecycle", "ok", 2*time.Second, 3*time.Second)
	RecordWebhookDelivery(t.Context(), "success", 4*time.Second)
	RecordWorkerEventsExpired(t.Context(), 1)
	data := collectedMetrics(t, reader)
	expected := map[string]struct {
		value   float64
		counter bool
		attrs   map[string]string
	}{
		"oma.river.jobs":                {1, true, map[string]string{"oma.river.kind": "sandbox_lifecycle_sweep", "oma.river.queue": "sandbox_lifecycle", "oma.outcome": "ok"}},
		"oma.river.job.duration":        {2, false, map[string]string{"oma.river.kind": "sandbox_lifecycle_sweep", "oma.river.queue": "sandbox_lifecycle", "oma.outcome": "ok"}},
		"oma.river.job.queue_wait":      {3, false, map[string]string{"oma.river.kind": "sandbox_lifecycle_sweep", "oma.river.queue": "sandbox_lifecycle"}},
		"oma.webhook.deliveries":        {1, true, map[string]string{"oma.outcome": "success"}},
		"oma.webhook.delivery.duration": {4, false, map[string]string{"oma.outcome": "success"}},
		"oma.worker_event.expired":      {1, true, nil},
	}
	if len(data) != len(expected) {
		t.Fatalf("metric names: %v", data)
	}
	for name, want := range expected {
		var attrs attribute.Set
		if want.counter {
			sum, ok := data[name].(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 || float64(sum.DataPoints[0].Value) != want.value {
				t.Fatalf("%s counter: %v", name, data[name])
			}
			attrs = sum.DataPoints[0].Attributes
		} else {
			hist, ok := data[name].(metricdata.Histogram[float64])
			if !ok || len(hist.DataPoints) != 1 || hist.DataPoints[0].Count != 1 || hist.DataPoints[0].Sum != want.value {
				t.Fatalf("%s histogram: %v", name, data[name])
			}
			attrs = hist.DataPoints[0].Attributes
		}
		if attrs.Len() != len(want.attrs) {
			t.Fatalf("%s attributes: %v", name, attrs)
		}
		for key, value := range want.attrs {
			got, ok := attrs.Value(attribute.Key(key))
			if !ok || got.Emit() != value {
				t.Fatalf("%s %s=%v", name, key, got)
			}
		}
	}
}

func TestStateMetricsFailureLabels(t *testing.T) {
	reader := metricReader(t)
	for _, reason := range []string{"database", "nats"} {
		RecordStateMetricsFailure(t.Context(), reason)
	}
	data := collectedMetrics(t, reader)
	sum, ok := data["oma.state_metrics.collect.failures"].(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 2 {
		t.Fatalf("failure measurements: %v", data)
	}
	for _, point := range sum.DataPoints {
		reason, ok := point.Attributes.Value(AttrReason)
		if !ok || (reason.AsString() != "database" && reason.AsString() != "nats") || point.Attributes.Len() != 1 || point.Value != 1 {
			t.Fatalf("failure attributes: %+v", point)
		}
	}
}

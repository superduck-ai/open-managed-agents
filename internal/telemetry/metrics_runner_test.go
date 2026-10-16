package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRunnerMetricsRejectNegativeQueueWait(t *testing.T) {
	reader := metricReader(t)
	RecordRunnerQueueWait(t.Context(), -time.Second)
	if data := collectedMetrics(t, reader); len(data) != 0 {
		t.Fatalf("negative queue wait recorded: %v", data)
	}
}

func TestRunnerMetricNamesAttributesAndValues(t *testing.T) {
	cases := []struct {
		name    string
		record  func(context.Context)
		attrs   map[string]string
		value   float64
		counter bool
	}{
		{"oma.runner.stage.duration", func(ctx context.Context) {
			RecordRunnerStage(ctx, RunnerTriggerLazyMCP, "prepare", "error", 2*time.Second)
		}, map[string]string{"oma.runner.trigger": "lazy_mcp", "oma.stage": "prepare", "oma.outcome": "error"}, 2, false},
		{"oma.runner.stale_recovery_claims", RecordRunnerStaleRecoveryClaim, nil, 1, true},
		{"oma.sandbox.ensure.duration", func(ctx context.Context) { RecordSandboxEnsure(ctx, RunnerTriggerPrewarm, "joined", 2*time.Second) }, map[string]string{"oma.runner.trigger": "prewarm", "oma.outcome": "joined"}, 2, false},
		{"oma.sandbox.provider.duration", func(ctx context.Context) { RecordSandboxProviderCall(ctx, "set_timeout", "not_found", 2*time.Second) }, map[string]string{"oma.operation": "set_timeout", "oma.outcome": "not_found"}, 2, false},
		{"oma.code_session.activation.duration", func(ctx context.Context) { RecordActivation(ctx, "timeout", 2*time.Second) }, map[string]string{"oma.outcome": "timeout"}, 2, false},
		{"oma.runner.queue_wait", func(ctx context.Context) { RecordRunnerQueueWait(ctx, 2*time.Second) }, nil, 2, false},
		{"oma.runner.runs", func(ctx context.Context) { RecordRunnerRun(ctx, RunnerTriggerQueue, "ok", true, 2*time.Second) }, map[string]string{"oma.runner.trigger": "queue", "oma.outcome": "ok", "oma.runner.recovered": "true"}, 1, true},
		{"oma.runner.run.duration", func(ctx context.Context) {
			RecordRunnerRun(ctx, RunnerTriggerStaleRecovery, "ok", false, 2*time.Second)
		}, map[string]string{"oma.runner.trigger": "stale_recovery", "oma.outcome": "ok", "oma.runner.recovered": "false"}, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := metricReader(t)
			tc.record(t.Context())
			data := collectedMetrics(t, reader)
			var attrs attribute.Set
			if tc.counter {
				sum, ok := data[tc.name].(metricdata.Sum[int64])
				if !ok || len(sum.DataPoints) != 1 || float64(sum.DataPoints[0].Value) != tc.value {
					t.Fatalf("counter: %v", data)
				}
				attrs = sum.DataPoints[0].Attributes
			} else {
				hist, ok := data[tc.name].(metricdata.Histogram[float64])
				if !ok || len(hist.DataPoints) != 1 || hist.DataPoints[0].Count != 1 || hist.DataPoints[0].Sum != tc.value {
					t.Fatalf("histogram: %v", data)
				}
				attrs = hist.DataPoints[0].Attributes
			}
			if attrs.Len() != len(tc.attrs) {
				t.Fatalf("attributes: %v", attrs)
			}
			for key, want := range tc.attrs {
				got, ok := attrs.Value(attribute.Key(key))
				if !ok || got.Emit() != want {
					t.Fatalf("%s = %v, want %s", key, got, want)
				}
			}
		})
	}
}

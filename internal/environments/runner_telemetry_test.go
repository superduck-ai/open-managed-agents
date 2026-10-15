package environments

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestRunnerTelemetry(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previousTrace, previousMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(provider)
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTrace)
		otel.SetMeterProvider(previousMeter)
		_ = provider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "failure without queue timestamp", false: "recovered success with linked queue"}[fail], func(t *testing.T) {
			start := len(recorder.Ended())
			work := db.EnvironmentWork{ExternalID: "work_private", SessionExternalID: "session_private", WorkspaceUUID: "workspace_private", EnvironmentExternalID: "environment_private"}
			if !fail {
				queued := time.Now().Add(-2 * time.Second)
				work.QueuedAt = &queued
				work.TraceParent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
			}
			ctx, run := startRunnerRun(t.Context(), work)
			if trace.SpanFromContext(ctx).SpanContext().TraceID() != run.span.SpanContext().TraceID() {
				t.Fatal("claim did not inherit root")
			}
			run.begin("prepare")
			run.markRecovered(!fail)
			if fail {
				run.end(errors.New("private failure"))
			} else {
				run.begin("e2b_create")
				run.end(nil)
			}
			spans := recorder.Ended()[start:]
			expected := []string{"oma.runner.claim", "oma.runner.prepare", "oma.runner.run"}
			if !fail {
				expected = []string{"oma.runner.claim", "oma.runner.prepare", "oma.runner.e2b_create", "oma.runner.run"}
			}
			if len(spans) != len(expected) {
				t.Fatalf("spans: %v", spans)
			}
			root := spans[len(spans)-1]
			if root.Parent().IsValid() {
				t.Fatal("runner root inherited parent")
			}
			attrs := map[string]string{}
			for i, span := range spans {
				if span.Name() != expected[i] {
					t.Fatalf("span %d = %s", i, span.Name())
				}
				if len(span.Events()) != 0 {
					t.Fatal("unexpected exception event")
				}
				for _, attr := range span.Attributes() {
					if strings.Contains(attr.Value.Emit(), "private failure") {
						t.Fatal("error text leaked")
					}
				}
				if i < len(spans)-1 && span.Parent().SpanID() != root.SpanContext().SpanID() {
					t.Fatal("stage parent mismatch")
				}
			}
			for _, attr := range root.Attributes() {
				attrs[string(attr.Key)] = attr.Value.Emit()
			}
			if fail && (attrs["oma.stage"] != "prepare" || root.Status().Code != codes.Error || attrs["error.type"] != "error") {
				t.Fatalf("failed root: %v, %v", attrs, root.Status())
			}
			if !fail && (len(root.Links()) != 1 || attrs["oma.runner.recovered"] != "true") {
				t.Fatalf("successful root: %v", attrs)
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(t.Context(), &data); err != nil {
				t.Fatal(err)
			}
			queueCount := uint64(0)
			for _, scope := range data.ScopeMetrics {
				for _, m := range scope.Metrics {
					if m.Name == "oma.runner.queue_wait" {
						for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
							queueCount += point.Count
						}
					}
				}
			}
			if fail && queueCount != 0 || !fail && queueCount != 1 {
				t.Fatalf("queue count = %d", queueCount)
			}
		})
	}
}

func TestRecoveryLaunchClassification(t *testing.T) {
	if isRecoveryLaunch(nil) || isRecoveryLaunch(&managedAgentLaunchPreparation{}) {
		t.Fatal("fresh launch marked recovered")
	}
	if !isRecoveryLaunch(&managedAgentLaunchPreparation{RecoveryCodeSessionID: "cse_recovery"}) {
		t.Fatal("recovery launch not marked")
	}
}

func TestEnvironmentWorkResponseOmitsTelemetry(t *testing.T) {
	now := time.Now()
	raw, err := json.Marshal(responseFromWork(db.EnvironmentWork{ExternalID: "work_test", SessionExternalID: "session_test", QueuedAt: &now, TraceParent: "private-trace-link"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"queued_at", "trace_parent", "private-trace-link", "QueuedAt", "TraceParent"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("internal telemetry in public response: %s", raw)
		}
	}
}

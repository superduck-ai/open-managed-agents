package tests

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

func TestEnvironmentWorkTelemetryPostgres(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	for _, replacement := range []string{"", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"} {
		t.Run(map[bool]string{true: "null recovery link preserves stored value", false: "recovery replaces link"}[replacement == ""], func(t *testing.T) {
			f := newSandboxLifecycleFixture(t)
			putCodeSessionWorkerState(t, f.app, f.code.ExternalID, `{"worker_epoch":`+f.epoch+`,"worker_status":"running"}`)
			work, err := f.app.db.GetLatestEnvironmentWorkForSession(t.Context(), f.code.WorkspaceUUID, f.code.EnvironmentExternalID, f.session.UUID)
			if err != nil {
				t.Fatal(err)
			}
			if work.QueuedAt == nil || !work.QueuedAt.Equal(work.CreatedAt) {
				t.Fatalf("initial queue timestamp: %+v", work.QueuedAt)
			}
			link, ok := telemetry.LinkFromTraceParent(work.TraceParent)
			if !ok {
				t.Fatal("stored request trace link missing")
			}
			matched := false
			for _, span := range recorder.Ended() {
				if span.Name() == "POST /v1/sessions" && span.SpanContext().TraceID() == link.SpanContext.TraceID() && span.SpanContext().SpanID() == link.SpanContext.SpanID() {
					matched = true
				}
			}
			if !matched {
				t.Fatal("stored link does not round-trip the create request span")
			}
			if changed, err := f.app.db.ScheduleEnvironmentSandboxRecoveryForCodeSession(t.Context(), f.code.ExternalID, "wrong-provider", nil, replacement); err != nil || changed {
				t.Fatalf("invalid recovery: %t, %v", changed, err)
			}
			unchanged, err := f.app.db.GetEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID)
			if err != nil || unchanged.TraceParent != work.TraceParent || unchanged.QueuedAt == nil || !unchanged.QueuedAt.Equal(*work.QueuedAt) {
				t.Fatalf("rejected recovery mutated telemetry: %+v, %v", unchanged, err)
			}
			if changed, err := f.app.db.ScheduleEnvironmentSandboxRecoveryForCodeSession(t.Context(), f.code.ExternalID, f.target.ProviderSandboxID, nil, replacement); err != nil || !changed {
				t.Fatalf("recovery: %t, %v", changed, err)
			}
			recovered, err := f.app.db.GetEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			expected := replacement
			if expected == "" {
				expected = work.TraceParent
			}
			if recovered.State != "queued" || recovered.QueuedAt == nil || !recovered.QueuedAt.After(*work.QueuedAt) || recovered.TraceParent != expected {
				t.Fatalf("recovery telemetry: %+v", recovered)
			}
			if _, err := f.app.db.AckEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID); err != nil {
				t.Fatal(err)
			}
			if changed, err := f.app.db.RequeueEnvironmentWorkIfRecoverable(t.Context(), recovered, time.Now().Add(time.Second)); err != nil || !changed {
				t.Fatalf("retry: %t, %v", changed, err)
			}
			retried, err := f.app.db.GetEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID)
			if err != nil || retried.QueuedAt == nil || !retried.QueuedAt.After(*recovered.QueuedAt) || retried.TraceParent != expected {
				t.Fatalf("retry telemetry: %+v, %v", retried, err)
			}
		})
	}
}

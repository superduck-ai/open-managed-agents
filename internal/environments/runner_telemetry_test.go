package environments

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestRunnerTelemetry(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	reader := telemetrytest.MetricReader(t)
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "failure without queue timestamp", false: "recovered success with linked queue"}[fail], func(t *testing.T) {
			start := len(recorder.Ended())
			work := db.EnvironmentWork{State: "queued", ExternalID: "work_private", SessionExternalID: "session_private", WorkspaceUUID: "workspace_private", EnvironmentExternalID: "environment_private"}
			if !fail {
				queued := time.Now().Add(-2 * time.Second)
				work.QueuedAt = &queued
				work.TraceParent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
			}
			ctx, run := startRunnerRun(t.Context(), work)
			if trace.SpanFromContext(ctx).SpanContext().TraceID() != run.span.SpanContext().TraceID() {
				t.Fatal("claim did not inherit root")
			}
			ctx = run.begin(ctx, "prepare")
			run.markRecovered(!fail)
			if fail {
				run.end(ctx, errors.New("private failure"))
			} else {
				ctx = run.begin(ctx, "e2b_create")
				run.end(ctx, nil)
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
			data := telemetrytest.Collect(t, reader)
			queueCount := uint64(0)
			if _, ok := data["oma.runner.queue_wait"]; ok {
				for _, point := range telemetrytest.Points(t, data, "oma.runner.queue_wait") {
					queueCount += point.Count
				}
			}
			if fail && queueCount != 0 || !fail && queueCount != 1 {
				t.Fatalf("queue count = %d", queueCount)
			}
		})
	}
}

func TestRunnerTriggerTelemetry(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	queued := time.Now().Add(-time.Hour)
	heartbeat := time.Now().Add(-90 * time.Second)
	stale := db.EnvironmentWork{State: "active", ExternalID: "work_stale", SessionExternalID: "session_stale", QueuedAt: &queued, LatestHeartbeatAt: &heartbeat, TraceParent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"}
	staleCtx, staleRun := startRunnerRun(t.Context(), stale)
	staleRun.end(staleCtx, errors.New("stale failure"))

	parentCtx, parent := telemetry.Start(t.Context(), "parent")
	lazyCtx, lazyRun := startSandboxRun(parentCtx, db.EnvironmentWork{State: "active", ExternalID: "work_lazy", SessionExternalID: "session_lazy"}, runnerTriggerLazyMCP)
	lazyRun.markRecovered(true)
	lazyCtx = lazyRun.begin(lazyCtx, "e2b_create")
	lazyRun.end(lazyCtx, nil)
	parent.End()

	triggers := map[string]sdktrace.ReadOnlySpan{}
	for _, span := range recorder.Ended() {
		if span.Name() != "oma.runner.run" {
			continue
		}
		for _, attr := range span.Attributes() {
			if attr.Key == "oma.runner.queue_wait_s" {
				t.Fatalf("queue wait recorded for %s", span.Name())
			}
			if attr.Key == "oma.runner.trigger" {
				triggers[attr.Value.Emit()] = span
			}
		}
	}
	if span := triggers["stale_recovery"]; span == nil || span.Parent().IsValid() || len(span.Links()) != 1 {
		t.Fatalf("stale recovery root: %v", triggers)
	}
	staleAttrs := map[string]attribute.Value{}
	for _, attr := range triggers["stale_recovery"].Attributes() {
		staleAttrs[string(attr.Key)] = attr.Value
	}
	if staleAttrs["oma.environment_work.state"].AsString() != "active" || staleAttrs["oma.environment_work.heartbeat_age_s"].AsFloat64() < 89 || staleAttrs["oma.environment_work.idle_s"].Type() != attribute.INVALID {
		t.Fatalf("stale recovery attributes: %v", staleAttrs)
	}
	if span := triggers["lazy_mcp"]; span == nil || span.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("lazy run is not a child span: %v", triggers)
	}
	for _, attr := range triggers["lazy_mcp"].Attributes() {
		if attr.Key == "oma.runner.recovered" && attr.Value.AsBool() {
			t.Fatal("lazy sandbox creation marked as recovery")
		}
	}
}

func TestRunnerStagePreservesCallerContext(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	reader := telemetrytest.MetricReader(t)
	ctx, run := startSandboxRun(t.Context(), db.EnvironmentWork{ExternalID: "work_ctx", SessionExternalID: "session_ctx"}, runnerTriggerPrewarm)
	type key struct{}
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.WithValue(ctx, key{}, "kept"), deadline)
	defer cancel()
	ctx = run.begin(ctx, "packages")
	if got, ok := ctx.Deadline(); !ok || !got.Equal(deadline) || ctx.Value(key{}) != "kept" {
		t.Fatal("stage dropped caller context")
	}
	run.end(ctx, nil)
	spans := recorder.Ended()
	if len(spans) != 3 || spans[1].Name() != "oma.runner.packages" || spans[1].Parent().SpanID() != run.span.SpanContext().SpanID() {
		t.Fatalf("stage spans: %v", spans)
	}
	data := telemetrytest.Collect(t, reader)
	if _, counted := data["oma.runner.runs"]; counted {
		t.Fatal("nested prewarm run counted as a separate run")
	}
	if points := telemetrytest.Points(t, data, "oma.runner.stage.duration"); len(points) != 2 {
		t.Fatalf("stage points: %v", points)
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

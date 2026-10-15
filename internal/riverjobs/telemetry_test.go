package riverjobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

func TestRiverTraceMetadata(t *testing.T) {
	parent := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	for _, raw := range []string{"{broken", "null", "[]", "42", `"text"`} {
		if got := withTraceParent([]byte(raw), parent); string(got) != raw {
			t.Fatalf("invalid object changed: %s", got)
		}
		if got := traceParentFromMetadata([]byte(raw)); got != "" {
			t.Fatalf("invalid trace metadata accepted: %q", got)
		}
	}
	if traceParentFromMetadata(nil) != "" {
		t.Fatal("missing metadata has link")
	}
	for _, raw := range []string{"", `{"keep":{"number":1234567890123456789},"oma_trace_parent":"previous"}`} {
		got := withTraceParent([]byte(raw), parent)
		if traceParentFromMetadata(got) != parent {
			t.Fatalf("trace link not merged: %s", got)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(got, &fields); err != nil {
			t.Fatal(err)
		}
		if raw != "" && string(fields["keep"]) != `{"number":1234567890123456789}` {
			t.Fatalf("unknown field changed: %s", fields["keep"])
		}
	}
}

func TestRiverOutcomeAndQueueWait(t *testing.T) {
	failure := errors.New("private failure")
	for _, tc := range []struct {
		err          error
		attempt, max int
		want         string
	}{
		{failure, 3, 3, "discarded"}, {failure, 1, 3, "retry"}, {river.JobCancel(failure), 3, 3, "cancelled"}, {river.JobSnooze(time.Second), 3, 3, "snoozed"}, {nil, 1, 3, "ok"},
	} {
		job := &rivertype.JobRow{Attempt: tc.attempt, MaxAttempts: tc.max}
		if got := riverOutcome(job, tc.err); got != tc.want {
			t.Fatalf("outcome=%s want %s", got, tc.want)
		}
		if (riverSpanError(tc.want, tc.err) == nil) != (tc.err == nil || tc.want == "snoozed") {
			t.Fatalf("span error for %s", tc.want)
		}
	}
	now := time.Now()
	earlier := now.Add(-time.Second)
	later := now.Add(time.Second)
	for _, tc := range []struct {
		attempted *time.Time
		want      time.Duration
	}{{nil, 0}, {&earlier, 0}, {&later, time.Second}} {
		if got := riverQueueWait(&rivertype.JobRow{ScheduledAt: now, AttemptedAt: tc.attempted}); got != tc.want {
			t.Fatalf("queue wait=%s want %s", got, tc.want)
		}
	}
}

func TestRiverMiddlewarePropagationAndCompletion(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	middleware := &telemetryMiddleware{}
	var insertion rivertype.JobInsertMiddleware = middleware
	var worker rivertype.WorkerMiddleware = middleware
	calls := 0
	invalid := []byte("null")
	params := []*rivertype.JobInsertParams{{Metadata: invalid}}
	failure := errors.New("private failure")
	_, err := insertion.InsertMany(t.Context(), params, func(context.Context) ([]*rivertype.JobInsertResult, error) { calls++; return nil, failure })
	if !errors.Is(err, failure) || calls != 1 || string(params[0].Metadata) != "null" {
		t.Fatal("untraced insertion behavior changed")
	}
	producerContext, producer := otel.Tracer("test").Start(t.Context(), "producer")
	defer producer.End()
	params = []*rivertype.JobInsertParams{{Metadata: []byte(`{"keep":true}`)}}
	_, err = insertion.InsertMany(producerContext, params, func(ctx context.Context) ([]*rivertype.JobInsertResult, error) {
		if !trace.SpanContextFromContext(ctx).Equal(producer.SpanContext()) {
			t.Fatal("insertion context changed")
		}
		return nil, nil
	})
	if err != nil || traceParentFromMetadata(params[0].Metadata) != telemetry.TraceParent(producerContext) {
		t.Fatal("insertion trace metadata missing")
	}
	for _, tc := range []struct {
		name string
		err  error
		code codes.Code
	}{{"retry", failure, codes.Error}, {"snoozed", river.JobSnooze(time.Second), codes.Unset}, {"ok", nil, codes.Unset}} {
		t.Run(tc.name, func(t *testing.T) {
			job := &rivertype.JobRow{ID: 1, Kind: "sandbox_lifecycle_sweep", Queue: "sandbox_lifecycle", Attempt: 1, MaxAttempts: 3, Metadata: params[0].Metadata}
			err := worker.Work(producerContext, job, func(ctx context.Context) error {
				if trace.SpanContextFromContext(ctx).TraceID() == producer.SpanContext().TraceID() {
					t.Fatal("job inherited producer parent")
				}
				return tc.err
			})
			if err != tc.err {
				t.Fatal("job result changed")
			}
			spans := recorder.Ended()
			span := spans[len(spans)-1]
			if span.Name() != "oma.river.sandbox_lifecycle_sweep" || span.Parent().IsValid() || len(span.Links()) != 1 || span.Links()[0].SpanContext.TraceID() != producer.SpanContext().TraceID() || span.Links()[0].SpanContext.SpanID() != producer.SpanContext().SpanID() || span.Status().Code != tc.code || len(span.Events()) != 0 {
				t.Fatalf("job span: %+v", span)
			}
			for _, attr := range span.Attributes() {
				if attr.Key == "oma.outcome" && attr.Value.AsString() != tc.name {
					t.Fatalf("outcome=%s", attr.Value.AsString())
				}
				if attr.Value.Emit() == "private failure" {
					t.Fatal("error text leaked")
				}
			}
		})
	}
}

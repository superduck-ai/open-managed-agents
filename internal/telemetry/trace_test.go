package telemetry

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestTraceLinks(t *testing.T) {
	for _, input := range []string{"", "invalid", "00-00000000000000000000000000000000-0000000000000000-01"} {
		if _, ok := LinkFromTraceParent(input); ok {
			t.Fatalf("accepted invalid traceparent %q", input)
		}
	}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	encoded := "00-" + parent.TraceID().String() + "-" + parent.SpanID().String() + "-01"
	ctx := trace.ContextWithSpanContext(t.Context(), parent)
	_, span := StartRoot(ctx, "test.root", encoded)
	span.End()
	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Parent().IsValid() || len(ended[0].Links()) != 1 || ended[0].Links()[0].SpanContext.TraceID() != parent.TraceID() || ended[0].SpanContext().TraceID() == parent.TraceID() {
		t.Fatalf("root/link contract failed: %v", ended)
	}
}

func TestTraceOutcomesExcludeErrorDetails(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	for _, tc := range []struct {
		err     error
		outcome string
	}{
		{errors.New("secret error details"), "error"},
		{fmt.Errorf("sensitive: %w", context.Canceled), "canceled"},
		{fmt.Errorf("sensitive: %w", context.DeadlineExceeded), "timeout"},
		{nil, "ok"},
	} {
		if got := Outcome(tc.err); got != tc.outcome {
			t.Errorf("outcome = %s, want %s", got, tc.outcome)
		}
		ctx, span := Start(t.Context(), "test.outcome")
		if _, ok := LinkFromTraceParent(TraceParent(ctx)); !ok {
			t.Fatal("traceparent did not round trip")
		}
		SetErrorStatus(span, tc.err)
		span.End()
	}
	for i, span := range recorder.Ended() {
		if len(span.Events()) != 0 {
			t.Fatal("error details recorded as span events")
		}
		if i == 1 {
			if span.Status().Code != codes.Unset || len(span.Attributes()) != 1 || span.Attributes()[0].Value.AsString() != OutcomeCanceled {
				t.Fatalf("canceled span marked as error: %v, %v", span.Status(), span.Attributes())
			}
		} else if i < 3 {
			if span.Status().Code != codes.Error || len(span.Attributes()) != 1 || span.Attributes()[0].Key != "error.type" || span.Attributes()[0].Value.AsString() != span.Status().Description {
				t.Fatalf("unexpected error span: %v, %v", span.Status(), span.Attributes())
			}
		} else if span.Status().Code != codes.Unset || len(span.Attributes()) != 0 {
			t.Fatal("success has error classification")
		}
	}
}

func TestMarkJoinsCurrentSpanAndLinksProducer(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	producer := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	ctx, parent := Start(t.Context(), "test.parent")
	Mark(ctx, "test.child", producer)
	parent.End()
	Mark(t.Context(), "test.root", producer)
	ended := recorder.Ended()
	if len(ended) != 3 {
		t.Fatalf("spans = %d", len(ended))
	}
	child, root := ended[0], ended[2]
	if child.Parent().SpanID() != parent.SpanContext().SpanID() || len(child.Links()) != 1 {
		t.Fatalf("child mark: parent=%v links=%v", child.Parent(), child.Links())
	}
	if root.Parent().IsValid() || len(root.Links()) != 1 || root.Links()[0].SpanContext.TraceID().String() != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("root mark: parent=%v links=%v", root.Parent(), root.Links())
	}
}

func TestInjectTraceParent(t *testing.T) {
	telemetrytest.SpanRecorder(t)
	headers := map[string]string{}
	InjectTraceParent(t.Context(), func(key, value string) { headers[key] = value })
	if len(headers) != 0 {
		t.Fatalf("injected without span: %v", headers)
	}
	ctx, span := Start(t.Context(), "test.inject")
	defer span.End()
	InjectTraceParent(ctx, func(key, value string) { headers[key] = value })
	if _, ok := LinkFromTraceParent(headers[TraceParentHeader]); !ok {
		t.Fatalf("headers = %v", headers)
	}
}

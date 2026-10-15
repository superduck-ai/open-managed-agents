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
		End(span, tc.err)
	}
	for i, span := range recorder.Ended() {
		if len(span.Events()) != 0 {
			t.Fatal("error details recorded as span events")
		}
		if i < 3 {
			if span.Status().Code != codes.Error || len(span.Attributes()) != 1 || span.Attributes()[0].Key != "error.type" || span.Attributes()[0].Value.AsString() != span.Status().Description {
				t.Fatalf("unexpected error span: %v, %v", span.Status(), span.Attributes())
			}
		} else if span.Status().Code != codes.Unset || len(span.Attributes()) != 0 {
			t.Fatal("success has error classification")
		}
	}
}

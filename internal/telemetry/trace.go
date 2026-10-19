package telemetry

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const TraceParentHeader = "traceparent"

const (
	OutcomeOK       = "ok"
	OutcomeError    = "error"
	OutcomeCanceled = "canceled"
	OutcomeTimeout  = "timeout"
)

func tracer() trace.Tracer {
	return otel.Tracer(InstrumentationName)
}

func StartRoot(ctx context.Context, name string, traceParent string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	options := []trace.SpanStartOption{trace.WithNewRoot(), trace.WithAttributes(attrs...)}
	if link, ok := LinkFromTraceParent(traceParent); ok {
		options = append(options, trace.WithLinks(link))
	}
	return tracer().Start(ctx, name, options...)
}

func Mark(ctx context.Context, name, linkedTraceParent string, attrs ...attribute.KeyValue) {
	options := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	if link, ok := LinkFromTraceParent(linkedTraceParent); ok {
		options = append(options, trace.WithLinks(link))
	}
	_, span := tracer().Start(ctx, name, options...)
	span.End()
}

func LinkFromTraceParent(traceParent string) (trace.Link, bool) {
	if traceParent == "" {
		return trace.Link{}, false
	}
	carrier := propagation.MapCarrier{TraceParentHeader: traceParent}
	spanContext := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(context.Background(), carrier))
	if !spanContext.IsValid() {
		return trace.Link{}, false
	}
	return trace.Link{SpanContext: spanContext}, true
}

func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

func SetErrorStatus(span trace.Span, err error) {
	if err == nil {
		return
	}
	outcome := Outcome(err)
	span.SetAttributes(AttrErrorType.String(outcome))
	if outcome != OutcomeCanceled {
		span.SetStatus(codes.Error, outcome)
	}
}

func Outcome(err error) string {
	switch {
	case err == nil:
		return OutcomeOK
	case errors.Is(err, context.Canceled):
		return OutcomeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return OutcomeTimeout
	default:
		return OutcomeError
	}
}

func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get(TraceParentHeader)
}

func InjectTraceParent(ctx context.Context, set func(key, value string)) {
	if traceParent := TraceParent(ctx); traceParent != "" {
		set(TraceParentHeader, traceParent)
	}
}

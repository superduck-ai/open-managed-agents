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

func tracer() trace.Tracer {
	return otel.Tracer(InstrumentationName)
}

func StartRoot(ctx context.Context, name string, traceParent string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	options := []trace.SpanStartOption{trace.WithNewRoot(), trace.WithAttributes(attrs...)}
	if link, ok := LinkFromTraceParent(traceParent); ok {
		options = append(options, trace.WithLinks(link))
	}
	ctx, span := tracer().Start(ctx, name, options...)
	return ctx, span
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
	ctx, span := tracer().Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, span
}

func End(span trace.Span, err error) {
	SetErrorStatus(span, err)
	span.End()
}

func SetErrorStatus(span trace.Span, err error) {
	if err != nil {
		outcome := Outcome(err)
		span.SetAttributes(attribute.String("error.type", outcome))
		span.SetStatus(codes.Error, outcome)
	}
}

func Outcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "error"
	}
}

func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get(TraceParentHeader)
}

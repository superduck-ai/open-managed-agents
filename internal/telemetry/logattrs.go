package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func LogContextAttrs(ctx context.Context) []slog.Attr {
	attrs := make([]slog.Attr, 0, 3)
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		attrs = append(attrs,
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	if requestID := httpapi.RequestID(ctx); requestID != "" {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	return attrs
}

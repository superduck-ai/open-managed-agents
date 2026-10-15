package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func RecordModelCall(ctx context.Context, measurement ModelCallMeasurement) {
	model := attribute.String("gen_ai.request.model", measurement.Model)
	source := AttrSource.String(measurement.Source)
	attrs := []attribute.KeyValue{model, source, attribute.Int("http.response.status_code", measurement.Status)}
	if measurement.ErrorType != "" {
		attrs = append(attrs, attribute.String("error.type", measurement.ErrorType))
	}
	if measurement.Duration > 0 {
		metrics().modelOperationDuration.Record(ctx, measurement.Duration.Seconds(), metric.WithAttributes(attrs...))
	}
	if measurement.TimeToFirstByte > 0 {
		metrics().modelTimeToFirstByte.Record(ctx, measurement.TimeToFirstByte.Seconds(), metric.WithAttributes(model, source))
	}
	if measurement.TimeToFirstToken > 0 {
		metrics().modelTimeToFirstToken.Record(ctx, measurement.TimeToFirstToken.Seconds(), metric.WithAttributes(model))
	}
	for _, usage := range []struct {
		kind  string
		value *int64
	}{
		{"input", measurement.Usage.Input},
		{"output", measurement.Usage.Output},
		{"cache_read", measurement.Usage.CacheRead},
		{"cache_creation", measurement.Usage.CacheCreation},
	} {
		if usage.value != nil {
			metrics().modelTokenUsage.Record(ctx, *usage.value, metric.WithAttributes(model, attribute.String("gen_ai.token.type", usage.kind)))
		}
	}
}

package httpapi

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

type sseStreamInstruments struct {
	streams  metric.Int64UpDownCounter
	duration metric.Float64Histogram
}

var sseStreamMetrics = telemetry.NewInstruments(func(meter metric.Meter) sseStreamInstruments {
	return sseStreamInstruments{
		streams:  telemetry.UpDownCounter(meter, "oma.sse.streams", "{stream}", "Open SSE streams"),
		duration: telemetry.Histogram(meter, "oma.sse.stream.duration", "s", "SSE stream duration", telemetry.LongWaitBuckets()),
	}
})

func ObserveSSEStream(ctx context.Context, kind string) func() {
	instruments := sseStreamMetrics.Get()
	attrs := metric.WithAttributes(telemetry.AttrStreamKind.String(kind))
	instruments.streams.Add(ctx, 1, attrs)
	startedAt := time.Now()
	return func() {
		instruments.streams.Add(ctx, -1, attrs)
		instruments.duration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
	}
}

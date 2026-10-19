package workerevents

import (
	"go.opentelemetry.io/otel/metric"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

type publishInstruments struct {
	workerEvent metric.Float64Histogram
}

var publishMetrics = telemetry.NewInstruments(func(meter metric.Meter) publishInstruments {
	return publishInstruments{
		workerEvent: telemetry.Histogram(meter, "oma.worker_event.publish.duration", "s", "Worker event publication duration", telemetry.LatencyBuckets),
	}
})

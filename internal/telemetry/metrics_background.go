package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

func RecordRiverJob(ctx context.Context, kind, queue, outcome string, duration, queueWait time.Duration) {
	attrs := metric.WithAttributes(AttrRiverKind.String(kind), AttrRiverQueue.String(queue), AttrOutcome.String(outcome))
	metrics().riverJobs.Add(ctx, 1, attrs)
	metrics().riverJobDuration.Record(ctx, duration.Seconds(), attrs)
	if queueWait > 0 {
		metrics().riverJobQueueWait.Record(ctx, queueWait.Seconds(), metric.WithAttributes(AttrRiverKind.String(kind), AttrRiverQueue.String(queue)))
	}
}

func RecordWebhookDelivery(ctx context.Context, outcome string, duration time.Duration) {
	attrs := metric.WithAttributes(AttrOutcome.String(outcome))
	metrics().webhookDeliveries.Add(ctx, 1, attrs)
	if duration > 0 {
		metrics().webhookDeliveryDuration.Record(ctx, duration.Seconds(), attrs)
	}
}

func RecordWorkerEventsExpired(ctx context.Context, count int64) {
	metrics().workerEventsExpired.Add(ctx, count)
}

func RecordStateMetricsFailure(ctx context.Context, reason string) {
	metrics().stateMetricsFailures.Add(ctx, 1, metric.WithAttributes(AttrReason.String(reason)))
}

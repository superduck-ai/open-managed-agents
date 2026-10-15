package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

func RecordSessionCreated(ctx context.Context, source string) {
	metrics().sessionCreated.Add(ctx, 1, metric.WithAttributes(AttrSource.String(source)))
}

func RecordSessionEventsAccepted(ctx context.Context, eventType string) {
	metrics().sessionEventsAccepted.Add(ctx, 1, metric.WithAttributes(AttrEventType.String(eventType)))
}

func RecordSessionEventsRejected(ctx context.Context, reason string) {
	metrics().sessionEventsRejected.Add(ctx, 1, metric.WithAttributes(AttrReason.String(reason)))
}

func RecordSessionHandoffFailure(ctx context.Context, reason string) {
	metrics().sessionHandoffFailures.Add(ctx, 1, metric.WithAttributes(AttrReason.String(reason)))
}

func RecordWorkerEventPublish(ctx context.Context, lane, outcome string, duration time.Duration) {
	metrics().workerEventPublishDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(AttrLane.String(lane), AttrOutcome.String(outcome)))
}

func RecordWorkerEventDelivered(ctx context.Context, lane string, count uint64, publishedAt time.Time) {
	if lane != "task" && lane != "reply" {
		return
	}
	if !publishedAt.IsZero() {
		delay := time.Since(publishedAt)
		if delay >= 0 {
			metrics().workerEventQueueWait.Record(ctx, delay.Seconds(), metric.WithAttributes(AttrLane.String(lane), AttrRedelivered.Bool(count > 1)))
		}
	}
	if count > 1 {
		metrics().workerEventRedeliveries.Add(ctx, 1, metric.WithAttributes(AttrLane.String(lane)))
	}
}

func RecordWorkerEventProcessed(ctx context.Context, lane string, duration time.Duration) {
	if duration < 0 || (lane != "task" && lane != "reply") {
		return
	}
	metrics().workerEventProcessedLatency.Record(ctx, duration.Seconds(), metric.WithAttributes(AttrLane.String(lane)))
}

func RecordWorkerEventUpdate(ctx context.Context, status, outcome string) {
	metrics().workerEventUpdates.Add(ctx, 1, metric.WithAttributes(AttrDeliveryStatus.String(status), AttrOutcome.String(outcome)))
}

func RecordSandboxResume(ctx context.Context, outcome string, duration time.Duration) {
	metrics().sandboxResumeDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(AttrOutcome.String(outcome)))
}

func RecordWorkerRegister(ctx context.Context, outcome string) {
	metrics().workerRegister.Add(ctx, 1, metric.WithAttributes(AttrOutcome.String(outcome)))
}

func RecordWorkerRegisterDelay(ctx context.Context, delay time.Duration) {
	metrics().workerRegisterDelay.Record(ctx, delay.Seconds())
}

func RecordWorkerHeartbeatRejected(ctx context.Context, reason string) {
	metrics().workerHeartbeatRejected.Add(ctx, 1, metric.WithAttributes(AttrReason.String(reason)))
}

func RecordToolPermissionDecision(ctx context.Context, decision, kind string) {
	metrics().toolPermissionDecisions.Add(ctx, 1, metric.WithAttributes(AttrDecision.String(decision), AttrToolKind.String(kind)))
}

func RecordToolPermissionResponse(ctx context.Context, behavior, source string, requestedAt time.Time) {
	metrics().toolPermissionResponses.Add(ctx, 1, metric.WithAttributes(AttrBehavior.String(behavior), AttrSource.String(source)))
	if (source == "tool-confirmation" || source == "custom-tool-result") && !requestedAt.IsZero() {
		delay := time.Since(requestedAt)
		if delay >= 0 {
			metrics().toolPermissionWait.Record(ctx, delay.Seconds(), metric.WithAttributes(AttrSource.String(source)))
		}
	}
}

func AddSSEStreams(ctx context.Context, kind string, delta int64) {
	metrics().sseStreams.Add(ctx, delta, metric.WithAttributes(AttrStreamKind.String(kind)))
}

func CloseSSEStream(ctx context.Context, kind string, startedAt time.Time) {
	AddSSEStreams(ctx, kind, -1)
	metrics().sseStreamDuration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(AttrStreamKind.String(kind)))
}

func RecordSSESubscriberDropped(ctx context.Context) { metrics().sseSubscribersDropped.Add(ctx, 1) }

func RecordFanoutPublishFailure(ctx context.Context) { metrics().fanoutPublishFailures.Add(ctx, 1) }

func RecordFanoutLag(ctx context.Context, delay time.Duration) {
	if delay >= 0 {
		metrics().fanoutLag.Record(ctx, delay.Seconds())
	}
}

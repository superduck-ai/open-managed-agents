package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

func RecordRunnerQueueWait(ctx context.Context, duration time.Duration) {
	if duration < 0 {
		return
	}
	metrics().runnerQueueWait.Record(ctx, duration.Seconds())
}

func RecordRunnerRun(ctx context.Context, outcome string, recovered bool, duration time.Duration) {
	attrs := metric.WithAttributes(AttrOutcome.String(outcome), AttrRecovered.Bool(recovered))
	metrics().runnerRuns.Add(ctx, 1, attrs)
	metrics().runnerRunDuration.Record(ctx, duration.Seconds(), attrs)
}

func RecordRunnerStage(ctx context.Context, stage, outcome string, duration time.Duration) {
	metrics().runnerStageDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(AttrStage.String(stage), AttrOutcome.String(outcome)))
}

func RecordSandboxProviderCall(ctx context.Context, operation, outcome string, duration time.Duration) {
	metrics().sandboxProviderDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(AttrOperation.String(operation), AttrOutcome.String(outcome)))
}

func RecordActivation(ctx context.Context, outcome string, duration time.Duration) {
	metrics().activationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(AttrOutcome.String(outcome)))
}

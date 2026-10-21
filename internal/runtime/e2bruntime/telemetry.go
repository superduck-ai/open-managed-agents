package e2bruntime

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

var providerCallDuration = telemetry.NewInstruments(func(meter metric.Meter) metric.Float64Histogram {
	return telemetry.Histogram(meter, "oma.sandbox.provider.duration", "s", "Sandbox provider duration", telemetry.LatencyBuckets)
})

func observeProviderCall(ctx context.Context, operation string, sandboxID string) (context.Context, func(error)) {
	ctx, span := telemetry.Start(ctx, "oma.e2b."+operation,
		telemetry.AttrOperation.String(operation),
		telemetry.AttrProviderSandboxID.String(sandboxID),
	)
	startedAt := time.Now()
	return ctx, func(err error) {
		defer span.End()
		outcome := telemetry.Outcome(err)
		if errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
			outcome = "not_found"
		}
		providerCallDuration.Get().Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(telemetry.AttrOperation.String(operation), telemetry.AttrOutcome.String(outcome)))
		telemetry.SetErrorStatus(span, err)
	}
}

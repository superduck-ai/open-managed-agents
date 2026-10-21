package webhooks

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

type deliveryInstruments struct {
	deliveries metric.Int64Counter
	duration   metric.Float64Histogram
}

var deliveryMetrics = telemetry.NewInstruments(func(meter metric.Meter) deliveryInstruments {
	return deliveryInstruments{
		deliveries: telemetry.Counter(meter, "oma.webhook.deliveries", "{delivery}", "Webhook deliveries"),
		duration:   telemetry.Histogram(meter, "oma.webhook.delivery.duration", "s", "Webhook delivery duration", telemetry.LatencyBuckets),
	}
})

func recordWebhookDelivery(ctx context.Context, result deliveryResult) {
	instruments := deliveryMetrics.Get()
	attrs := metric.WithAttributes(telemetry.AttrOutcome.String(result.outcome))
	instruments.deliveries.Add(ctx, 1, attrs)
	if result.duration > 0 {
		instruments.duration.Record(ctx, result.duration.Seconds(), attrs)
	}
}

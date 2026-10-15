package telemetry

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}

var longWaitBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200, 21600, 86400}

var tokenBuckets = []float64{16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576}

type TokenUsage struct {
	Input         *int64
	Output        *int64
	CacheRead     *int64
	CacheCreation *int64
}

type ModelCallMeasurement struct {
	Model            string
	Source           string
	Status           int
	ErrorType        string
	Duration         time.Duration
	TimeToFirstByte  time.Duration
	TimeToFirstToken time.Duration
	Usage            TokenUsage
}

type instruments struct {
	stateMetricsFailures        metric.Int64Counter
	riverJobs                   metric.Int64Counter
	riverJobDuration            metric.Float64Histogram
	riverJobQueueWait           metric.Float64Histogram
	webhookDeliveries           metric.Int64Counter
	webhookDeliveryDuration     metric.Float64Histogram
	workerEventsExpired         metric.Int64Counter
	modelOperationDuration      metric.Float64Histogram
	modelTimeToFirstByte        metric.Float64Histogram
	modelTimeToFirstToken       metric.Float64Histogram
	modelTokenUsage             metric.Int64Histogram
	runnerQueueWait             metric.Float64Histogram
	runnerRuns                  metric.Int64Counter
	runnerRunDuration           metric.Float64Histogram
	runnerStageDuration         metric.Float64Histogram
	sandboxProviderDuration     metric.Float64Histogram
	activationDuration          metric.Float64Histogram
	sessionCreated              metric.Int64Counter
	sessionEventsAccepted       metric.Int64Counter
	sessionEventsRejected       metric.Int64Counter
	sessionHandoffFailures      metric.Int64Counter
	workerEventPublishDuration  metric.Float64Histogram
	workerEventQueueWait        metric.Float64Histogram
	workerEventRedeliveries     metric.Int64Counter
	workerEventProcessedLatency metric.Float64Histogram
	workerEventUpdates          metric.Int64Counter
	sandboxResumeDuration       metric.Float64Histogram
	workerRegister              metric.Int64Counter
	workerRegisterDelay         metric.Float64Histogram
	workerHeartbeatRejected     metric.Int64Counter
	toolPermissionDecisions     metric.Int64Counter
	toolPermissionResponses     metric.Int64Counter
	toolPermissionWait          metric.Float64Histogram
	sseStreams                  metric.Int64UpDownCounter
	sseStreamDuration           metric.Float64Histogram
	sseSubscribersDropped       metric.Int64Counter
	fanoutPublishFailures       metric.Int64Counter
	fanoutLag                   metric.Float64Histogram

	httpRequestDuration metric.Float64Histogram
}

var (
	instrumentsOnce sync.Once
	registry        instruments
)

func metrics() *instruments {
	instrumentsOnce.Do(func() {
		meter := otel.Meter(InstrumentationName)
		registry = instruments{
			stateMetricsFailures:        int64Counter(meter, "oma.state_metrics.collect.failures", "{failure}", "Operational state collection failures"),
			riverJobs:                   int64Counter(meter, "oma.river.jobs", "{job}", "River jobs"),
			riverJobDuration:            float64Histogram(meter, "oma.river.job.duration", "s", "River job duration", latencyBuckets),
			riverJobQueueWait:           float64Histogram(meter, "oma.river.job.queue_wait", "s", "River job queue wait", longWaitBuckets),
			webhookDeliveries:           int64Counter(meter, "oma.webhook.deliveries", "{delivery}", "Webhook deliveries"),
			webhookDeliveryDuration:     float64Histogram(meter, "oma.webhook.delivery.duration", "s", "Webhook delivery duration", latencyBuckets),
			workerEventsExpired:         int64Counter(meter, "oma.worker_event.expired", "{code_session}", "Code sessions expired after worker event retention"),
			modelOperationDuration:      float64Histogram(meter, "gen_ai.client.operation.duration", "s", "Model operation duration", latencyBuckets),
			modelTimeToFirstByte:        float64Histogram(meter, "oma.model.time_to_first_byte", "s", "Model time to first byte", latencyBuckets),
			modelTimeToFirstToken:       float64Histogram(meter, "oma.model.time_to_first_token", "s", "Model time to first token", latencyBuckets),
			modelTokenUsage:             int64Histogram(meter, "gen_ai.client.token.usage", "{token}", "Model token usage", tokenBuckets),
			runnerQueueWait:             float64Histogram(meter, "oma.runner.queue_wait", "s", "Runner queue wait", longWaitBuckets),
			runnerRuns:                  int64Counter(meter, "oma.runner.runs", "{run}", "Runner runs"),
			runnerRunDuration:           float64Histogram(meter, "oma.runner.run.duration", "s", "Runner run duration", latencyBuckets),
			runnerStageDuration:         float64Histogram(meter, "oma.runner.stage.duration", "s", "Runner stage duration", latencyBuckets),
			sandboxProviderDuration:     float64Histogram(meter, "oma.sandbox.provider.duration", "s", "Sandbox provider duration", latencyBuckets),
			activationDuration:          float64Histogram(meter, "oma.code_session.activation.duration", "s", "Code session activation duration", latencyBuckets),
			sessionCreated:              int64Counter(meter, "oma.session.created", "{session}", "Sessions created"),
			sessionEventsAccepted:       int64Counter(meter, "oma.session.events.accepted", "{event}", "Session events accepted"),
			sessionEventsRejected:       int64Counter(meter, "oma.session.events.rejected", "{request}", "Session event requests rejected"),
			sessionHandoffFailures:      int64Counter(meter, "oma.session.handoff.failures", "{failure}", "Session handoff failures"),
			workerEventPublishDuration:  float64Histogram(meter, "oma.worker_event.publish.duration", "s", "Worker event publication duration", latencyBuckets),
			workerEventQueueWait:        float64Histogram(meter, "oma.worker_event.queue_wait", "s", "Worker event queue wait", longWaitBuckets),
			workerEventRedeliveries:     int64Counter(meter, "oma.worker_event.redeliveries", "{event}", "Worker event redeliveries"),
			workerEventProcessedLatency: float64Histogram(meter, "oma.worker_event.processed.latency", "s", "Worker event processing latency", longWaitBuckets),
			workerEventUpdates:          int64Counter(meter, "oma.worker_event.updates", "{update}", "Worker event updates"),
			sandboxResumeDuration:       float64Histogram(meter, "oma.sandbox.resume.duration", "s", "Sandbox resume duration", latencyBuckets),
			workerRegister:              int64Counter(meter, "oma.worker.register", "{registration}", "Worker registrations"),
			workerRegisterDelay:         float64Histogram(meter, "oma.worker.register.delay", "s", "Worker registration delay", latencyBuckets),
			workerHeartbeatRejected:     int64Counter(meter, "oma.worker.heartbeat.rejected", "{heartbeat}", "Worker heartbeats rejected"),
			toolPermissionDecisions:     int64Counter(meter, "oma.tool_permission.decisions", "{decision}", "Tool permission decisions"),
			toolPermissionResponses:     int64Counter(meter, "oma.tool_permission.responses", "{response}", "Tool permission responses"),
			toolPermissionWait:          float64Histogram(meter, "oma.tool_permission.wait", "s", "Tool permission wait", longWaitBuckets),
			sseStreams:                  int64UpDownCounter(meter, "oma.sse.streams", "{stream}", "Open SSE streams"),
			sseStreamDuration:           float64Histogram(meter, "oma.sse.stream.duration", "s", "SSE stream duration", longWaitBuckets),
			sseSubscribersDropped:       int64Counter(meter, "oma.sse.subscribers.dropped", "{subscriber}", "SSE subscribers dropped"),
			fanoutPublishFailures:       int64Counter(meter, "oma.fanout.publish.failures", "{failure}", "Fanout publication failures"),
			fanoutLag:                   float64Histogram(meter, "oma.fanout.lag", "s", "Fanout lag", latencyBuckets),

			httpRequestDuration: float64Histogram(meter, "http.server.request.duration", "s", "HTTP server request duration", latencyBuckets),
		}
	})
	return &registry
}

func float64Histogram(meter metric.Meter, name, unit, description string, buckets []float64) metric.Float64Histogram {
	histogram, err := meter.Float64Histogram(name,
		metric.WithUnit(unit),
		metric.WithDescription(description),
		metric.WithExplicitBucketBoundaries(buckets...),
	)
	if err != nil {
		otel.Handle(err)
		return noop.Float64Histogram{}
	}
	return histogram
}

func RecordHTTPRequest(ctx context.Context, method, route string, status int, duration time.Duration) {
	metrics().httpRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("http.request.method", method),
		attribute.String("http.route", route),
		attribute.Int("http.response.status_code", status),
	))
}

func int64Counter(meter metric.Meter, name, unit, description string) metric.Int64Counter {
	instrument, err := meter.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(description))
	if err != nil {
		otel.Handle(err)
		return noop.Int64Counter{}
	}
	return instrument
}

func int64UpDownCounter(meter metric.Meter, name, unit, description string) metric.Int64UpDownCounter {
	instrument, err := meter.Int64UpDownCounter(name, metric.WithUnit(unit), metric.WithDescription(description))
	if err != nil {
		otel.Handle(err)
		return noop.Int64UpDownCounter{}
	}
	return instrument
}

func int64Histogram(meter metric.Meter, name, unit, description string, buckets []float64) metric.Int64Histogram {
	histogram, err := meter.Int64Histogram(name, metric.WithUnit(unit), metric.WithDescription(description), metric.WithExplicitBucketBoundaries(buckets...))
	if err != nil {
		otel.Handle(err)
		return noop.Int64Histogram{}
	}
	return histogram
}

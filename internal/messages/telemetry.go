package messages

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

type modelInstruments struct {
	operationDuration metric.Float64Histogram
	timeToFirstByte   metric.Float64Histogram
	timeToFirstToken  metric.Float64Histogram
	tokenUsage        metric.Int64Histogram
}

var modelMetrics = telemetry.NewInstruments(func(meter metric.Meter) modelInstruments {
	return modelInstruments{
		operationDuration: telemetry.Histogram(meter, "gen_ai.client.operation.duration", "s", "Model operation duration", telemetry.LatencyBuckets()),
		timeToFirstByte:   telemetry.Histogram(meter, "oma.model.time_to_first_byte", "s", "Model time to first byte", telemetry.LatencyBuckets()),
		timeToFirstToken:  telemetry.Histogram(meter, "oma.model.time_to_first_token", "s", "Model time to first token", telemetry.LatencyBuckets()),
		tokenUsage:        telemetry.Int64Histogram(meter, "gen_ai.client.token.usage", "{token}", "Model token usage", telemetry.TokenBuckets()),
	}
})

var modelStopReasons = telemetry.NewEnum("end_turn", "tool_use", "max_tokens", "stop_sequence", "pause_turn", "refusal")

type tokenUsage struct {
	Input         *int64
	Output        *int64
	CacheRead     *int64
	CacheCreation *int64
}

type modelCallMeasurement struct {
	Model            string
	Source           string
	Status           int
	ErrorType        string
	Duration         time.Duration
	TimeToFirstByte  time.Duration
	TimeToFirstToken time.Duration
	Usage            tokenUsage
}

type modelCall struct {
	ctx            context.Context
	span           trace.Span
	endSpan        func()
	model          string
	source         string
	startedAt      time.Time
	firstByteAt    time.Time
	status         int
	preflightError string
	observation    *responseObservation
}

func startModelCall(ctx context.Context, principal auth.Principal, model string) *modelCall {
	source := "api_key"
	if principal.CodeSessionExternalID != "" {
		source = "code_session"
	}
	ctx, span := telemetry.Start(ctx, "chat "+model,
		attribute.String("gen_ai.request.model", model),
		telemetry.AttrSource.String(source),
		telemetry.AttrCodeSessionID.String(principal.CodeSessionExternalID),
		telemetry.AttrSessionID.String(principal.PublicSessionExternalID),
	)
	return &modelCall{ctx: ctx, span: span, endSpan: func() { span.End() }, model: model, source: source, startedAt: time.Now()}
}

func (c *modelCall) failBeforeProxy(errorType string) {
	c.preflightError = errorType
}

func (c *modelCall) received(status int) {
	c.status = status
	c.firstByteAt = time.Now()
}

func (c *modelCall) errorType() string {
	if c.preflightError != "" || c.observation == nil {
		return c.preflightError
	}
	if errorType := modelObservationErrorType(c.observation.result.ErrorType); errorType != "" {
		return errorType
	}
	if c.status >= 400 {
		return "http_error"
	}
	return ""
}

func (c *modelCall) end() {
	measurement := modelCallMeasurement{
		Model:     c.model,
		Source:    c.source,
		Status:    c.status,
		ErrorType: c.errorType(),
		Duration:  time.Since(c.startedAt),
	}
	if !c.firstByteAt.IsZero() {
		measurement.TimeToFirstByte = c.firstByteAt.Sub(c.startedAt)
	}
	if c.observation != nil {
		measurement.TimeToFirstToken = c.observation.timeToFirstToken(c.startedAt)
		measurement.Usage = modelTokenUsage(c.observation.result.Usage)
		c.span.SetAttributes(modelResponseAttributes(c.observation, measurement)...)
	}
	recordModelCall(c.ctx, measurement)
	if c.status != 0 {
		c.span.SetAttributes(attribute.Int("http.response.status_code", c.status))
	}
	if measurement.ErrorType != "" {
		c.span.SetAttributes(attribute.String("error.type", measurement.ErrorType))
		if measurement.ErrorType != telemetry.OutcomeCanceled {
			c.span.SetStatus(codes.Error, measurement.ErrorType)
		}
	}
	c.endSpan()
}

func recordModelCall(ctx context.Context, measurement modelCallMeasurement) {
	instruments := modelMetrics.Get()
	model := attribute.String("gen_ai.request.model", measurement.Model)
	source := telemetry.AttrSource.String(measurement.Source)
	attrs := []attribute.KeyValue{model, source}
	if measurement.Status != 0 {
		attrs = append(attrs, attribute.Int("http.response.status_code", measurement.Status))
	}
	if measurement.ErrorType != "" {
		attrs = append(attrs, attribute.String("error.type", measurement.ErrorType))
	}
	if measurement.Duration > 0 {
		instruments.operationDuration.Record(ctx, measurement.Duration.Seconds(), metric.WithAttributes(attrs...))
	}
	if measurement.TimeToFirstByte > 0 {
		instruments.timeToFirstByte.Record(ctx, measurement.TimeToFirstByte.Seconds(), metric.WithAttributes(model, source))
	}
	if measurement.TimeToFirstToken > 0 {
		instruments.timeToFirstToken.Record(ctx, measurement.TimeToFirstToken.Seconds(), metric.WithAttributes(model))
	}
	for _, usage := range tokenUsageEntries(measurement.Usage) {
		instruments.tokenUsage.Record(ctx, *usage.value, metric.WithAttributes(model, attribute.String("gen_ai.token.type", usage.kind)))
	}
}

type tokenUsageEntry struct {
	kind      string
	attribute string
	value     *int64
}

func tokenUsageEntries(usage tokenUsage) []tokenUsageEntry {
	entries := []tokenUsageEntry{
		{"input", "gen_ai.usage.input_tokens", usage.Input},
		{"output", "gen_ai.usage.output_tokens", usage.Output},
		{"cache_read", "gen_ai.usage.cache_read_input_tokens", usage.CacheRead},
		{"cache_creation", "gen_ai.usage.cache_creation_input_tokens", usage.CacheCreation},
	}
	measured := entries[:0]
	for _, entry := range entries {
		if entry.value != nil {
			measured = append(measured, entry)
		}
	}
	return measured
}

func modelResponseAttributes(observation *responseObservation, measurement modelCallMeasurement) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	for _, usage := range tokenUsageEntries(measurement.Usage) {
		attrs = append(attrs, attribute.Int64(usage.attribute, *usage.value))
	}
	if reason := observation.stopReason; reason != "" {
		attrs = append(attrs, attribute.StringSlice("gen_ai.response.finish_reasons", []string{modelStopReasons.Normalize(reason)}))
	}
	if measurement.TimeToFirstToken > 0 {
		attrs = append(attrs, attribute.Float64("oma.model.time_to_first_token_s", measurement.TimeToFirstToken.Seconds()))
	}
	if measurement.TimeToFirstByte > 0 {
		attrs = append(attrs, attribute.Float64("oma.model.time_to_first_byte_s", measurement.TimeToFirstByte.Seconds()))
	}
	if id := observation.result.UpstreamRequestID; id != "" {
		attrs = append(attrs, attribute.String("oma.model.upstream_request_id", id))
	}
	return attrs
}

func modelTokenUsage(usage codesessions.ModelRequestUsage) tokenUsage {
	return tokenUsage{Input: usage.InputTokens, Output: usage.OutputTokens, CacheRead: usage.CacheReadInputTokens, CacheCreation: usage.CacheCreationInputTokens}
}

func modelObservationErrorType(errorType string) string {
	switch errorType {
	case "invalid_response", "incomplete_response", "provider_error":
		return "stream_error"
	case "cancelled":
		return telemetry.OutcomeCanceled
	default:
		return errorType
	}
}

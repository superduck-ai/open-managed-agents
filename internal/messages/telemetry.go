package messages

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

type modelCall struct {
	ctx         context.Context
	span        trace.Span
	endSpan     func()
	model       string
	source      string
	startedAt   time.Time
	firstByteAt time.Time
	status      int
	errorType   string
	observation *responseObservation
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

func (c *modelCall) fail(errorType string) {
	if c.errorType == "" {
		c.errorType = errorType
	}
}

func (c *modelCall) received(status int) {
	c.status = status
	c.firstByteAt = time.Now()
	if status >= 400 {
		c.fail("http_error")
	}
}

func (c *modelCall) end() {
	measurement := telemetry.ModelCallMeasurement{
		Model:     c.model,
		Source:    c.source,
		Status:    c.status,
		ErrorType: c.errorType,
		Duration:  time.Since(c.startedAt),
	}
	if !c.firstByteAt.IsZero() {
		measurement.TimeToFirstByte = c.firstByteAt.Sub(c.startedAt)
	}
	if c.observation != nil {
		measurement.TimeToFirstToken = c.observation.timeToFirstToken(c.startedAt)
		measurement.Usage = modelTokenUsage(c.observation.result.Usage)
		c.fail(modelObservationErrorType(c.observation.result.ErrorType))
		measurement.ErrorType = c.errorType
	}
	telemetry.RecordModelCall(c.ctx, measurement)
	c.span.SetAttributes(attribute.Int("http.response.status_code", c.status))
	if c.errorType != "" {
		c.span.SetAttributes(attribute.String("error.type", c.errorType))
		c.span.SetStatus(codes.Error, c.errorType)
	}
	c.endSpan()
}

func modelTokenUsage(usage codesessions.ModelRequestUsage) telemetry.TokenUsage {
	return telemetry.TokenUsage{Input: usage.InputTokens, Output: usage.OutputTokens, CacheRead: usage.CacheReadInputTokens, CacheCreation: usage.CacheCreationInputTokens}
}

func modelObservationErrorType(errorType string) string {
	switch errorType {
	case "invalid_response", "incomplete_response", "provider_error":
		return "stream_error"
	default:
		return errorType
	}
}

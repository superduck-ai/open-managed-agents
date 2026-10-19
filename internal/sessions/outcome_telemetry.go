package sessions

import (
	"context"
	"encoding/json"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

var (
	sessionStopReasons = telemetry.NewEnum("end_turn", "requires_action", "retries_exhausted")
	sessionErrorTypes  = telemetry.NewEnum(
		"unknown_error", "billing_error", "model_overloaded_error", "model_rate_limited_error", "model_request_failed_error",
		"mcp_connection_failed_error", "mcp_authentication_failed_error", "mcp_egress_blocked_error",
	)
	sessionRetryStatuses = telemetry.NewEnum("retrying", "exhausted", "terminal")
)

type sessionOutcomePayload struct {
	StopReason *struct {
		Type string `json:"type"`
	} `json:"stop_reason"`
	Error *struct {
		Type        string `json:"type"`
		RetryStatus *struct {
			Type string `json:"type"`
		} `json:"retry_status"`
	} `json:"error"`
}

func decodeSessionOutcome(payload json.RawMessage) sessionOutcomePayload {
	var decoded sessionOutcomePayload
	_ = json.Unmarshal(payload, &decoded)
	return decoded
}

func (h *Handler) observeSessionOutcomeEvents(ctx context.Context, events []db.SessionEvent) {
	for _, event := range events {
		switch {
		case strings.HasPrefix(event.EventType, "session.status_"):
			h.observeSessionStatus(ctx, event)
		case event.EventType == "session.error":
			h.observeSessionError(ctx, event)
		}
	}
}

func (h *Handler) observeSessionStatus(ctx context.Context, event db.SessionEvent) {
	attrs := []any{"session_id", event.SessionExternalID, "event_id", event.ExternalID, "event_type", event.EventType}
	markAttrs := []attribute.KeyValue{telemetry.AttrSessionID.String(event.SessionExternalID), telemetry.AttrEventID.String(event.ExternalID), telemetry.AttrEventType.String(event.EventType)}
	if payload := decodeSessionOutcome(event.Payload); payload.StopReason != nil {
		stopReason := sessionStopReasons.Normalize(payload.StopReason.Type)
		if event.EventType == "session.status_idle" {
			sessionMetrics.Get().turns.Add(ctx, 1, metric.WithAttributes(telemetry.AttrStopReason.String(stopReason)))
		}
		attrs = append(attrs, "stop_reason", stopReason)
		markAttrs = append(markAttrs, telemetry.AttrStopReason.String(stopReason))
	}
	h.logger.DebugContext(ctx, "session status event", attrs...)
	telemetry.Mark(ctx, "oma.session.status", "", markAttrs...)
}

func (h *Handler) observeSessionError(ctx context.Context, event db.SessionEvent) {
	errorType, retryStatus := telemetry.OtherValue, telemetry.OtherValue
	if payload := decodeSessionOutcome(event.Payload); payload.Error != nil {
		errorType = sessionErrorTypes.Normalize(payload.Error.Type)
		if payload.Error.RetryStatus != nil {
			retryStatus = sessionRetryStatuses.Normalize(payload.Error.RetryStatus.Type)
		}
	}
	sessionMetrics.Get().errors.Add(ctx, 1, metric.WithAttributes(telemetry.AttrErrorType.String(errorType), telemetry.AttrRetryStatus.String(retryStatus)))
	h.logger.WarnContext(ctx, "session error event", "session_id", event.SessionExternalID, "event_id", event.ExternalID, "error_type", errorType, "retry_status", retryStatus)
	_, span := telemetry.Start(ctx, "oma.session.error", telemetry.AttrSessionID.String(event.SessionExternalID), telemetry.AttrEventID.String(event.ExternalID),
		telemetry.AttrErrorType.String(errorType), telemetry.AttrRetryStatus.String(retryStatus))
	span.SetStatus(codes.Error, errorType)
	span.End()
}

package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

type sessionInstruments struct {
	eventsAccepted        metric.Int64Counter
	eventsRejected        metric.Int64Counter
	turns                 metric.Int64Counter
	errors                metric.Int64Counter
	handoffFailures       metric.Int64Counter
	inputQueueWait        metric.Float64Histogram
	sseSubscribersDropped metric.Int64Counter
	fanoutPublishFailures metric.Int64Counter
}

var sessionMetrics = telemetry.NewInstruments(func(meter metric.Meter) sessionInstruments {
	return sessionInstruments{
		eventsAccepted:        telemetry.Counter(meter, "oma.session.events.accepted", "{event}", "Session events accepted"),
		eventsRejected:        telemetry.Counter(meter, "oma.session.events.rejected", "{request}", "Session event requests rejected"),
		turns:                 telemetry.Counter(meter, "oma.session.turns", "{turn}", "Session turns ended by stop reason"),
		errors:                telemetry.Counter(meter, "oma.session.errors", "{error}", "Session error events"),
		handoffFailures:       telemetry.Counter(meter, "oma.session.handoff.failures", "{failure}", "Session handoff failures"),
		inputQueueWait:        telemetry.Histogram(meter, "oma.session.input.queue_wait", "s", "Session input wait before worker processing", telemetry.LongWaitBuckets()),
		sseSubscribersDropped: telemetry.Counter(meter, "oma.sse.subscribers.dropped", "{subscriber}", "SSE subscribers dropped"),
		fanoutPublishFailures: telemetry.Counter(meter, "oma.fanout.publish.failures", "{failure}", "Fanout publication failures"),
	}
})

func sessionEventRejectionReason(err error) string {
	switch {
	case errors.Is(err, db.ErrInvalidState):
		return "archived"
	case errors.Is(err, db.ErrSessionToolUseNotPending):
		return "tool_use_not_pending"
	case errors.Is(err, db.ErrSessionInputConflict):
		return "conflict"
	case errors.Is(err, db.ErrSessionInputPaused):
		return "paused"
	default:
		return telemetry.OtherValue
	}
}

type sessionInputShape struct {
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
	} `json:"content"`
}

func annotateSessionInputShape(ctx context.Context, inputs []json.RawMessage) {
	eventTypes, blockTypes := map[string]bool{}, map[string]bool{}
	size := 0
	for _, raw := range inputs {
		size += len(raw)
		var input sessionInputShape
		if json.Unmarshal(raw, &input) != nil {
			continue
		}
		eventTypes[clientInputTypeName(input.Type)] = true
		for _, block := range input.Content {
			blockTypes[telemetry.Token(block.Type)] = true
		}
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.StringSlice("oma.input.event_types", slices.Sorted(maps.Keys(eventTypes))),
		attribute.StringSlice("oma.input.block_types", slices.Sorted(maps.Keys(blockTypes))),
		attribute.Int("oma.input.bytes", size),
		attribute.Int("oma.input.events", len(inputs)),
	)
}

func clientInputTypeName(eventType string) string {
	if maevents.IsClientInput(eventType) {
		return eventType
	}
	return telemetry.OtherValue
}

func recordSessionEventsRejected(ctx context.Context, reason string) {
	trace.SpanFromContext(ctx).SetAttributes(telemetry.AttrReason.String(reason))
	sessionMetrics.Get().eventsRejected.Add(ctx, 1, metric.WithAttributes(telemetry.AttrReason.String(reason)))
}

func recordSessionHandoffFailure(ctx context.Context, err error) {
	reason := telemetry.OtherValue
	if errors.Is(err, codesessions.ErrWorkerEventUnavailable) {
		reason = "worker_event_unavailable"
	}
	sessionMetrics.Get().handoffFailures.Add(ctx, 1, metric.WithAttributes(telemetry.AttrReason.String(reason)))
}

func recordAcceptedSessionEvents(ctx context.Context, events []db.SessionEvent) {
	counter := sessionMetrics.Get().eventsAccepted
	for _, event := range events {
		if maevents.IsClientInput(event.EventType) {
			counter.Add(ctx, 1, metric.WithAttributes(telemetry.AttrEventType.String(event.EventType)))
		}
	}
}

func recordSessionInputQueueWait(ctx context.Context, events []db.SessionEvent) {
	histogram := sessionMetrics.Get().inputQueueWait
	for _, event := range events {
		if !event.ProcessedAt.IsZero() && !event.CreatedAt.IsZero() {
			telemetry.RecordWait(ctx, histogram, event.ProcessedAt.Sub(event.CreatedAt))
		}
	}
}

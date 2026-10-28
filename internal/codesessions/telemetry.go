package codesessions

import (
	"context"
	"maps"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type codeSessionInstruments struct {
	activation              metric.Float64Histogram
	workerRegisterDelay     metric.Float64Histogram
	workerEventQueueWait    metric.Float64Histogram
	workerEventRedeliveries metric.Int64Counter
	workerEventProcessed    metric.Float64Histogram
	workerEventUpdates      metric.Int64Counter
	sandboxMCP              metric.Float64Histogram
	toolPermissionDecisions metric.Int64Counter
	toolPermissionResponses metric.Int64Counter
	toolPermissionWait      metric.Float64Histogram
	brainClaimPoll          metric.Float64Histogram
	brainClaimQueueWait     metric.Float64Histogram
}

var codeSessionMetrics = telemetry.NewInstruments(func(meter metric.Meter) codeSessionInstruments {
	return codeSessionInstruments{
		activation:              telemetry.Histogram(meter, "oma.code_session.activation.duration", "s", "Code session activation duration", telemetry.LatencyBuckets()),
		workerRegisterDelay:     telemetry.Histogram(meter, "oma.worker.register.delay", "s", "Worker registration delay", telemetry.LatencyBuckets()),
		workerEventQueueWait:    telemetry.Histogram(meter, "oma.worker_event.queue_wait", "s", "Worker event queue wait", telemetry.LongWaitBuckets()),
		workerEventRedeliveries: telemetry.Counter(meter, "oma.worker_event.redeliveries", "{event}", "Worker event redeliveries"),
		workerEventProcessed:    telemetry.Histogram(meter, "oma.worker_event.processed.latency", "s", "Worker event processing latency", telemetry.LongWaitBuckets()),
		workerEventUpdates:      telemetry.Counter(meter, "oma.worker_event.updates", "{update}", "Worker event updates"),
		sandboxMCP:              telemetry.Histogram(meter, "oma.sandbox_mcp.duration", "s", "Sandbox MCP gateway call duration", telemetry.LatencyBuckets()),
		toolPermissionDecisions: telemetry.Counter(meter, "oma.tool_permission.decisions", "{decision}", "Tool permission decisions"),
		toolPermissionResponses: telemetry.Counter(meter, "oma.tool_permission.responses", "{response}", "Tool permission responses"),
		toolPermissionWait:      telemetry.Histogram(meter, "oma.tool_permission.wait", "s", "Tool permission wait", telemetry.LongWaitBuckets()),
		brainClaimPoll:          telemetry.Histogram(meter, "oma.brain_claim.poll.duration", "s", "Brain claim long-poll duration", telemetry.LatencyBuckets()),
		brainClaimQueueWait:     telemetry.Histogram(meter, "oma.brain_claim.queue_wait", "s", "Wait from Brain worker request to claim", telemetry.LatencyBuckets()),
	}
})

var workerDeliveryStatuses = telemetry.NewEnum("received", "processing", "processed")

func observeBrainClaimPoll(ctx context.Context, startedAt time.Time, found bool, err error) {
	outcome := "empty"
	switch {
	case err != nil:
		outcome = telemetry.Outcome(err)
	case found:
		outcome = "claimed"
	}
	observeOperation(ctx, codeSessionMetrics.Get().brainClaimPoll, outcome, startedAt)
}

func observeBrainClaimIssued(ctx context.Context, workerID string, claim db.CodeSessionClaim) {
	telemetry.Mark(ctx, "oma.brain_claim.issue", claim.TraceParent,
		telemetry.AttrCodeSessionID.String(claim.CodeSessionExternalID), telemetry.AttrSessionID.String(claim.SessionExternalID),
		telemetry.AttrWorkerEpoch.Int64(claim.WorkerEpoch), attribute.String("oma.brain.worker_id", workerID))
	if claim.RequestedAt != nil {
		telemetry.RecordWait(ctx, codeSessionMetrics.Get().brainClaimQueueWait, time.Since(*claim.RequestedAt))
	}
}

func observeOperation(ctx context.Context, histogram metric.Float64Histogram, outcome string, startedAt time.Time) {
	histogram.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(telemetry.AttrOutcome.String(outcome)))
}

func (h *Handler) observeWorkerEventDelivery(ctx context.Context, codeSession db.CodeSession, epoch int64, delivery workerevents.Delivery, eventID string) {
	telemetry.Mark(ctx, "oma.worker_event.deliver", delivery.TraceParent,
		telemetry.AttrCodeSessionID.String(codeSession.ExternalID), telemetry.AttrSessionID.String(codeSession.SessionExternalID),
		telemetry.AttrEventID.String(eventID), telemetry.AttrLane.String(delivery.Lane), telemetry.AttrWorkerEpoch.Int64(epoch), telemetry.AttrRedelivered.Bool(delivery.DeliveryCount > 1))
	instruments := codeSessionMetrics.Get()
	if !delivery.PublishedAt.IsZero() {
		telemetry.RecordWait(ctx, instruments.workerEventQueueWait, time.Since(delivery.PublishedAt),
			metric.WithAttributes(telemetry.AttrLane.String(delivery.Lane), telemetry.AttrRedelivered.Bool(delivery.DeliveryCount > 1)))
	}
	if delivery.DeliveryCount > 1 {
		instruments.workerEventRedeliveries.Add(ctx, 1, metric.WithAttributes(telemetry.AttrLane.String(delivery.Lane)))
	}
}

func observeWorkerEventAck(ctx context.Context, codeSessionID, eventID string, reference workerevents.AckRef) {
	if reference.PublishedAt.IsZero() {
		return
	}
	telemetry.RecordWait(ctx, codeSessionMetrics.Get().workerEventProcessed, time.Since(reference.PublishedAt), metric.WithAttributes(telemetry.AttrLane.String(reference.Lane)))
	telemetry.Mark(ctx, "oma.worker_event.ack", reference.TraceParent,
		telemetry.AttrCodeSessionID.String(codeSessionID), telemetry.AttrEventID.String(eventID), telemetry.AttrLane.String(reference.Lane))
}

func recordWorkerEventUpdate(ctx context.Context, status, outcome string) {
	codeSessionMetrics.Get().workerEventUpdates.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDeliveryStatus.String(workerDeliveryStatuses.Normalize(status)), telemetry.AttrOutcome.String(outcome)))
}

func annotateWorkerEventBatch(ctx context.Context, count int, eventTypes []string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	types := map[string]bool{}
	for _, eventType := range eventTypes {
		types[telemetry.Token(eventType)] = true
	}
	span.SetAttributes(
		attribute.Int("oma.worker_event.count", count),
		attribute.StringSlice("oma.worker_event.types", slices.Sorted(maps.Keys(types))),
	)
}

func (s *Service) observeInputWorkerAvailability(ctx context.Context, codeSession db.CodeSession, inputs int, now time.Time) {
	leaseActive := codeSession.WorkerLeaseExpiresAt != nil && codeSession.WorkerLeaseExpiresAt.After(now)
	trace.SpanFromContext(ctx).SetAttributes(
		telemetry.AttrConnectionStatus.String(codeSession.ConnectionStatus),
		telemetry.AttrWorkerStatus.String(codeSession.WorkerStatus),
		attribute.Bool("oma.worker.lease_active", leaseActive),
	)
	if codeSession.ConnectionStatus == "connected" && leaseActive || usesBrainClaimTransport(codeSession) {
		return
	}
	attrs := []any{"code_session_id", codeSession.ExternalID, "session_id", codeSession.SessionExternalID,
		"connection_status", codeSession.ConnectionStatus, "worker_status", codeSession.WorkerStatus, "lease_active", leaseActive, "inputs", inputs}
	if codeSession.WorkerLeaseExpiresAt != nil {
		attrs = append(attrs, "lease_expired_s", now.Sub(*codeSession.WorkerLeaseExpiresAt).Seconds())
	}
	s.logger.WarnContext(ctx, "session input queued without connected worker", attrs...)
}

func observeWorkerRegistered(ctx context.Context, claims SessionCredentialClaims) {
	if claims.WorkerEpoch <= 0 || claims.IssuedAt == nil {
		return
	}
	delay := time.Since(claims.IssuedAt.Time)
	if delay < 0 || delay > 15*time.Minute {
		return
	}
	codeSessionMetrics.Get().workerRegisterDelay.Record(ctx, delay.Seconds())
	trace.SpanFromContext(ctx).SetAttributes(attribute.Float64("oma.worker.register_delay_s", delay.Seconds()))
}

var userAnsweredToolPermissionSources = map[string]bool{"tool-confirmation": true, "custom-tool-result": true}

func observeToolPermissionDecision(ctx context.Context, codeSessionID, toolName string, permission resolvedToolPermission, identity toolIdentity) {
	codeSessionMetrics.Get().toolPermissionDecisions.Add(ctx, 1, metric.WithAttributes(telemetry.AttrDecision.String(string(permission)), telemetry.AttrToolKind.String(identity.Kind)))
	telemetry.Mark(ctx, "oma.tool_permission.request", "", telemetry.AttrCodeSessionID.String(codeSessionID),
		telemetry.AttrDecision.String(string(permission)), telemetry.AttrToolKind.String(identity.Kind), telemetry.AttrToolName.String(toolName),
		attribute.String("oma.tool.server_name", identity.ServerName), attribute.String("oma.tool.normalized_name", identity.ToolName))
}

func observeToolPermissionResponse(ctx context.Context, codeSessionID string, behavior resolvedToolPermission, source string, requestedAt time.Time) {
	instruments := codeSessionMetrics.Get()
	instruments.toolPermissionResponses.Add(ctx, 1, metric.WithAttributes(telemetry.AttrBehavior.String(string(behavior)), telemetry.AttrSource.String(source)))
	attrs := []attribute.KeyValue{telemetry.AttrCodeSessionID.String(codeSessionID), telemetry.AttrBehavior.String(string(behavior)), telemetry.AttrSource.String(source)}
	if !requestedAt.IsZero() {
		wait := time.Since(requestedAt)
		attrs = append(attrs, attribute.Float64("oma.tool_permission.wait_s", wait.Seconds()))
		if userAnsweredToolPermissionSources[source] {
			telemetry.RecordWait(ctx, instruments.toolPermissionWait, wait, metric.WithAttributes(telemetry.AttrSource.String(source)))
		}
	}
	telemetry.Mark(ctx, "oma.tool_permission.respond", "", attrs...)
}

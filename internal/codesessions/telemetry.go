package codesessions

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func (h *Handler) observeWorkerEventDelivery(ctx context.Context, codeSession db.CodeSession, epoch int64, delivery workerevents.Delivery, eventID string) {
	_, span := telemetry.StartRoot(ctx, "oma.worker_event.deliver", delivery.TraceParent,
		telemetry.AttrCodeSessionID.String(codeSession.ExternalID), telemetry.AttrSessionID.String(codeSession.SessionExternalID),
		telemetry.AttrEventID.String(eventID), telemetry.AttrLane.String(delivery.Lane), telemetry.AttrWorkerEpoch.Int64(epoch), telemetry.AttrRedelivered.Bool(delivery.DeliveryCount > 1))
	telemetry.RecordWorkerEventDelivered(ctx, delivery.Lane, delivery.DeliveryCount, delivery.PublishedAt)
	span.End()
}

func workerRegisterDelay(claims SessionCredentialClaims) (time.Duration, bool) {
	if claims.WorkerEpoch <= 0 || claims.IssuedAt == nil {
		return 0, false
	}
	delay := time.Since(claims.IssuedAt.Time)
	if delay < 0 || delay > 15*time.Minute {
		return 0, false
	}
	return delay, true
}

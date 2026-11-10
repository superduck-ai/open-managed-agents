package codesessions

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type workerStreamAction uint8

const (
	workerStreamPublish workerStreamAction = iota
	workerStreamSkip
	workerStreamStop
)

func (h *Handler) prepareWorkerStreamDelivery(ctx context.Context, codeSession db.CodeSession, delivery workerevents.Delivery) workerStreamAction {
	envelope := delivery.Envelope
	if envelope.Version != 2 || envelope.CodeSessionID != codeSession.ExternalID {
		h.logger.WarnContext(ctx, "reject code session worker event envelope", "code_session_id", codeSession.ExternalID, "event_id", envelope.EventID)
		return workerStreamStop
	}
	if isDSHCodeSession(codeSession) && envelope.EventType == "user.message" {
		if err := h.service.workerEvents.DoubleAck(ctx, delivery.AckSubject); err != nil {
			return workerStreamStop
		}
		if envelope.PayloadRef != nil {
			h.service.triggerPayloadCleanupNow(ctx, envelope.PayloadRef.CleanupJobID)
		}
		return workerStreamSkip
	}
	if envelope.IsExpired(time.Now().UTC()) {
		if err := h.service.expireWorkerEvent(ctx, codeSession, envelope); err != nil {
			h.logger.ErrorContext(ctx, "expire code session worker event", "code_session_id", codeSession.ExternalID, "error", err)
		}
		return workerStreamStop
	}
	return workerStreamPublish
}

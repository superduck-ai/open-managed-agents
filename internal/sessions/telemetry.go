package sessions

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

func sessionEventRejectionReason(err error) string {
	switch {
	case errors.Is(err, db.ErrInvalidState):
		return "archived"
	case errors.Is(err, db.ErrSessionInputConflict):
		return "conflict"
	default:
		return "other"
	}
}

func sessionHandoffFailureReason(err error) string {
	if errors.Is(err, codesessions.ErrWorkerEventUnavailable) {
		return "worker_event_unavailable"
	}
	return "other"
}

func recordAcceptedSessionEvents(ctx context.Context, events []db.SessionEvent) {
	for _, event := range events {
		eventType := "other"
		if maevents.IsClientInput(event.EventType) {
			eventType = event.EventType
		}
		telemetry.RecordSessionEventsAccepted(ctx, eventType)
	}
}

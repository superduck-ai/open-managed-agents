package db

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/superduck-ai/yourbatis"
)

func rejectPausedSessionInput(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, events []SessionEvent) error {
	if primary.Status != "idle" || !slices.ContainsFunc(events, func(event SessionEvent) bool { return event.EventType == "user.message" }) {
		return nil
	}
	status, found, err := NewSessionEventMapper(executor).FindLatestStatus(ctx, session.WorkspaceUUID, session.ExternalID, primary.ExternalID)
	if err != nil || !found {
		return err
	}
	var payload sessionStatusPayload
	if err := json.Unmarshal(status.Payload, &payload); err != nil {
		return err
	}
	if payload.StopReason != nil && (payload.StopReason.Type == "requires_action" || payload.StopReason.Type == "budget_reached") {
		return ErrSessionInputPaused
	}
	return nil
}

func processExistingSessionInputTx(ctx context.Context, executor yourbatis.Executor, session Session, existing sessionEventRow, event SessionEvent) (SessionEvent, bool, error) {
	if existing.EventType != "user.message" || event.EventType != "user.message" || existing.ProcessedAt != nil || event.ProcessedAt.IsZero() {
		return SessionEvent{}, false, nil
	}
	updated, applied, err := NewSessionEventMapper(executor).MarkInputProcessed(ctx, session.WorkspaceUUID, session.ExternalID, event.ExternalID, event.ProcessedAt)
	return updated.event(), applied, err
}

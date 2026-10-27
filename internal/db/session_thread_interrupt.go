package db

import (
	"context"
	"encoding/json"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

func settledThreadInterruptsTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker codeSessionInputStateRow, events []SessionEvent) (map[string]bool, error) {
	settled := make(map[string]bool)
	for _, event := range events {
		if event.EventType != "user.interrupt" || event.ThreadExternalID == nil {
			continue
		}
		row, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, *event.ThreadExternalID)
		if err != nil {
			return nil, mapNoRows(err)
		}
		target := row.thread()
		if target.Status == "terminated" || target.ArchivedAt != nil {
			return nil, ErrSessionInterruptTargetInvalid
		}
		if target.ExternalID == primary.ExternalID || target.Status != "idle" {
			continue
		}
		status, found, err := NewSessionEventMapper(executor).FindLatestStatus(ctx, session.WorkspaceUUID, session.ExternalID, target.ExternalID)
		if err != nil {
			return nil, err
		}
		if !found || status.EventType != "session.thread_status_idle" {
			continue
		}
		var payload sessionStatusPayload
		if err := json.Unmarshal(status.Payload, &payload); err != nil {
			return nil, err
		}
		noop, err := settledChildInterrupt(session, primary, worker, event, target, payload.StopReason)
		if err != nil {
			return nil, err
		}
		if noop {
			settled[event.ExternalID] = true
		}
	}
	return settled, nil
}

func settledChildInterrupt(session Session, primary SessionThread, worker codeSessionInputStateRow, event SessionEvent, target SessionThread, reason *sessionStopReason) (bool, error) {
	if event.EventType != "user.interrupt" || event.ThreadExternalID == nil || *event.ThreadExternalID != target.ExternalID || target.ExternalID == primary.ExternalID || target.ParentThreadExternalID == nil || target.WorkspaceUUID != session.WorkspaceUUID || target.SessionExternalID != session.ExternalID {
		return false, ErrSessionInterruptTargetInvalid
	}
	if target.Status == "terminated" || target.ArchivedAt != nil {
		return false, ErrSessionInterruptTargetInvalid
	}
	if primary.Status == "terminated" || target.Status != "idle" || reason == nil || reason.Type != "end_turn" && reason.Type != "budget_reached" {
		return false, nil
	}
	pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, target.ExternalID)
	return err == nil && len(pending) == 0, err
}

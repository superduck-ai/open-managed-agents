package db

import (
	"context"
	"encoding/json"
	"slices"
	"time"

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
	if payload.StopReason != nil && (payload.StopReason.Type == "requires_action" || payload.StopReason.Type == "budget_reached" && session.BudgetReachedAt != nil) {
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

func (d *DB) ProcessSessionInputsForWorker(ctx context.Context, codeSession CodeSession, epoch int64, inputIDs []string) ([]SessionEvent, error) {
	if epoch <= 0 {
		return nil, ErrWorkerEpochMismatch
	}
	var events []SessionEvent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), codeSession.WorkspaceUUID, codeSession.SessionExternalID)
		if err != nil {
			return err
		}
		if session.ArchivedAt != nil {
			return ErrInvalidState
		}
		worker, found, err := NewCodeSessionMapper(executor).LockCodeSessionByExternalID(ctx, codeSession.ExternalID)
		if err != nil {
			return err
		}
		if !found || worker.WorkspaceUUID != session.WorkspaceUUID || worker.OrganizationUUID != session.OrganizationUUID || worker.SessionUUID != session.UUID {
			return ErrNotFound
		}
		if worker.Status != "active" {
			return ErrInvalidState
		}
		if worker.CurrentWorkerEpoch != epoch {
			return ErrWorkerEpochMismatch
		}
		now := time.Now().UTC()
		mapper := NewSessionEventMapper(executor)
		for _, id := range inputIDs {
			row, applied, err := mapper.MarkInputProcessed(ctx, session.WorkspaceUUID, session.ExternalID, id, now)
			if err != nil {
				return err
			}
			if applied {
				events = append(events, row.event())
			}
		}
		if len(events) > 0 {
			return NewCodeSessionMapper(executor).ResetIdleSinceForSession(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID, false)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

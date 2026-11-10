package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type DSHInputClaim struct {
	Event             SessionEvent
	CancelRequestedAt *time.Time
}

func (d *DB) ClaimDSHInput(ctx context.Context, scope DSHScope) (DSHInputClaim, SessionEventChanges, error) {
	var claim DSHInputClaim
	var changes SessionEventChanges
	_, found, err := NewDSHExecutionMapper(d.mapperDB).NextExecution(ctx, scope)
	if err != nil {
		return claim, changes, err
	}
	if !found {
		worker, exists, err := NewDSHTranscriptMapper(d.mapperDB).ReadWorker(ctx, scope)
		if err != nil {
			return claim, changes, err
		}
		return claim, changes, validateDSHWorker(scope, worker, exists)
	}
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, primary, err := lockDSHSession(ctx, executor, scope)
		if err != nil {
			return err
		}
		mapper := NewDSHExecutionMapper(executor)
		execution, found, err := mapper.NextExecution(ctx, scope)
		if err != nil || !found {
			return err
		}
		if execution.Status == "accepted" {
			summary, err := mapper.Summary(ctx, scope)
			if err != nil {
				return err
			}
			permissions, err := mapper.Permissions(ctx, scope)
			if err != nil {
				return err
			}
			status, _ := dshPrimaryState(summary, permissions, session.BudgetReachedAt != nil)
			if status != "running" {
				return nil
			}
			event, changed, err := startDSHInput(ctx, executor, scope, execution, session.BudgetReachedAt != nil)
			if err != nil {
				return err
			}
			if changed {
				changes.Events = append(changes.Events, event)
			}
			events, err := reduceDSHState(ctx, executor, &session, primary, scope, "claim:"+execution.InputID)
			if err != nil {
				return err
			}
			changes.Events = append(changes.Events, events...)
		}
		event, err := NewSessionEventMapper(executor).FindByExternalID(ctx, scope.WorkspaceUUID, scope.SessionID, execution.InputID)
		if err != nil {
			return err
		}
		claim = DSHInputClaim{Event: event.event(), CancelRequestedAt: execution.CancelRequestedAt}
		return nil
	})
	return claim, changes, err
}

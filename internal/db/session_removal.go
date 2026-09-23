package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

// Removal may cancel accepted work that the worker has not started. Serialize
// with input acceptance and worker reports using the same Session → Worker locks.
func prepareSessionRemovalTx(ctx context.Context, executor yourbatis.Executor, workspaceUUID, sessionID string) error {
	session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), workspaceUUID, sessionID)
	if err != nil {
		return err
	}
	if session.Status != "running" && session.Status != "rescheduling" {
		return nil
	}
	worker, found, err := NewCodeSessionMapper(executor).LockLatestInputState(ctx, workspaceUUID, session.UUID)
	if err != nil {
		return err
	}
	if found && worker.Status != "terminated" && worker.WorkerTurnStarted {
		return ErrInvalidState
	}
	if found {
		if _, err := NewCodeSessionMapper(executor).TerminateByExternalID(ctx, session.OrganizationUUID, workspaceUUID, worker.ExternalID); err != nil {
			return err
		}
	}
	primary, err := NewSessionThreadMapper(executor).FindPrimary(ctx, workspaceUUID, sessionID)
	if err != nil {
		return mapNoRows(err)
	}
	if _, err := NewSessionThreadMapper(executor).SetStatus(ctx, workspaceUUID, sessionID, primary.ExternalID, "terminated"); err != nil {
		return err
	}
	_, err = NewSessionMapper(executor).SetStatus(ctx, workspaceUUID, sessionID, "terminated")
	return err
}

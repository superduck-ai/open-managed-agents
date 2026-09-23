package db

import (
	"context"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/yourbatis"
)

// SessionRemoval reports accepted work cancelled by archive or delete. The
// caller purges the worker queue and publishes StatusEvents after commit.
type SessionRemoval struct {
	Session               Session
	TerminatedCodeSession string
	StatusEvents          []SessionEvent
}

// Removal may cancel accepted work that the worker has not started. Serialize
// with input acceptance and worker reports using the same Session → Worker locks.
func prepareSessionRemovalTx(ctx context.Context, executor yourbatis.Executor, workspaceUUID, sessionID string) (SessionRemoval, error) {
	session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), workspaceUUID, sessionID)
	if err != nil {
		return SessionRemoval{}, err
	}
	if session.Status != "running" && session.Status != "rescheduling" {
		return SessionRemoval{}, nil
	}
	codeSessions := NewCodeSessionMapper(executor)
	worker, found, err := codeSessions.LockLatestInputState(ctx, workspaceUUID, session.UUID)
	if err != nil {
		return SessionRemoval{}, err
	}
	var removal SessionRemoval
	if found {
		if worker.Status != "terminated" && worker.WorkerTurnStarted {
			return SessionRemoval{}, ErrInvalidState
		}
		if _, err := codeSessions.TerminateByExternalID(ctx, session.OrganizationUUID, workspaceUUID, worker.ExternalID); err != nil {
			return SessionRemoval{}, err
		}
		removal.TerminatedCodeSession = worker.ExternalID
	}
	eventID, err := ids.New("sevt_")
	if err != nil {
		return SessionRemoval{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	removal.StatusEvents, err = insertSessionEventsTx(ctx, executor, session, []SessionEvent{{
		UUID: uuid.NewV4().String(), ExternalID: eventID, EventType: "session.status_terminated", CreatedAt: now, ProcessedAt: now,
	}}, false)
	return removal, err
}

package db

import (
	"context"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/yourbatis"
)

type SessionEventChanges struct {
	Events                []SessionEvent
	RetiredCodeSessionIDs []string
}

type SessionRemoval struct {
	Session        Session
	CodeSessionIDs []string
	StatusEvents   []SessionEvent
}

func prepareSessionRemovalTx(ctx context.Context, executor yourbatis.Executor, workspaceUUID, sessionID string, archive bool) (SessionRemoval, error) {
	session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), workspaceUUID, sessionID)
	if err != nil {
		return SessionRemoval{}, err
	}
	if session.Status != "running" && session.Status != "rescheduling" && (!archive || session.Status != "idle") {
		return SessionRemoval{}, nil
	}
	codeSessions := NewCodeSessionMapper(executor)
	worker, found, err := codeSessions.LockLatestInputState(ctx, workspaceUUID, session.UUID)
	if err != nil {
		return SessionRemoval{}, err
	}
	var removal SessionRemoval
	if found && session.Status != "idle" && worker.Status != "terminated" && worker.WorkerTurnStarted {
		return SessionRemoval{}, ErrInvalidState
	}
	eventID, err := ids.New("sevt_")
	if err != nil {
		return SessionRemoval{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	result, err := insertSessionEventsTx(ctx, executor, session, []SessionEvent{{
		UUID: uuid.NewV4().String(), ExternalID: eventID, EventType: "session.status_terminated", CreatedAt: now, ProcessedAt: now,
	}})
	removal.StatusEvents = result.Events
	return removal, err
}

func (d *DB) retireSessionWorkersTx(ctx context.Context, executor yourbatis.Executor, removal *SessionRemoval) error {
	session := removal.Session
	ids, err := NewCodeSessionMapper(executor).TerminateBySession(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID)
	if err != nil {
		return err
	}
	removal.CodeSessionIDs = ids
	return nil
}

func (d *DB) IsSessionRetired(ctx context.Context, organizationUUID, workspaceUUID, sessionUUID string) (bool, error) {
	return NewSessionMapper(d.mapperDB).IsRetired(ctx, organizationUUID, workspaceUUID, sessionUUID)
}

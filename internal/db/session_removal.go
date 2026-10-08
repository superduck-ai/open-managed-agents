package db

import (
	"context"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/yourbatis"
)

type SessionRemoval struct {
	Session          Session
	CodeSessionIDs   []string
	CleanupScheduled bool
	StatusEvents     []SessionEvent
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
	removal.StatusEvents, err = insertSessionEventsTx(ctx, executor, session, []SessionEvent{{
		UUID: uuid.NewV4().String(), ExternalID: eventID, EventType: "session.status_terminated", CreatedAt: now, ProcessedAt: now,
	}})
	return removal, err
}

func (d *DB) ConfigureSessionCleanup(enqueue func(context.Context, *yourbatis.Tx, SessionRemoval) error) {
	d.sessionCleanup = enqueue
}

func (d *DB) retireSessionWorkersTx(ctx context.Context, executor yourbatis.Executor, removal *SessionRemoval) error {
	session := removal.Session
	ids, err := NewCodeSessionMapper(executor).TerminateBySession(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID)
	if err != nil {
		return err
	}
	removal.CodeSessionIDs = ids
	if len(ids) == 0 || d.sessionCleanup == nil {
		return nil
	}
	if err := d.sessionCleanup(ctx, executor.(*yourbatis.Tx), *removal); err != nil {
		return err
	}
	removal.CleanupScheduled = true
	return nil
}

func (d *DB) retireSessionForEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent) error {
	for _, event := range events {
		if event.EventType == "session.status_terminated" {
			return d.retireSessionWorkersTx(ctx, executor, &SessionRemoval{Session: session})
		}
	}
	return nil
}

func (d *DB) persistSessionEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent) ([]SessionEvent, error) {
	created, err := insertSessionEventsTx(ctx, executor, session, events)
	if err != nil {
		return nil, err
	}
	return created, d.retireSessionForEventsTx(ctx, executor, session, created)
}

func (d *DB) persistSessionHistoryTx(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent) ([]SessionEvent, error) {
	created, err := insertSessionHistoryTx(ctx, executor, session, events)
	if err != nil {
		return nil, err
	}
	return created, d.retireSessionForEventsTx(ctx, executor, session, created)
}

func (d *DB) IsSessionRetired(ctx context.Context, organizationUUID, workspaceUUID, sessionUUID string) (bool, error) {
	return NewSessionMapper(d.mapperDB).IsRetired(ctx, organizationUUID, workspaceUUID, sessionUUID)
}

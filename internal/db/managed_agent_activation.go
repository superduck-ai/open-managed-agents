package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/superduck-ai/yourbatis"
)

// ManagedAgentEventTx keeps Session events, threads and worker delivery in one
// transaction, using the same Session -> Code Session lock order for every writer.
type ManagedAgentEventTx struct {
	executor                      yourbatis.Executor
	codeSessionMapper             CodeSessionMapper
	codeSessionInboundEventMapper CodeSessionInboundEventMapper
	sessionMapper                 SessionMapper
	sessionEventMapper            SessionEventMapper
	sessionThreadMapper           SessionThreadMapper
}

// WithManagedAgentEventTx owns the database transaction lifecycle while
// leaving event conversion and activation policy to the resource services.
func (d *DB) WithManagedAgentEventTx(
	ctx context.Context,
	fn func(ManagedAgentEventTx) error,
) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return fn(ManagedAgentEventTx{
			executor:                      executor,
			codeSessionMapper:             NewCodeSessionMapper(executor),
			codeSessionInboundEventMapper: NewCodeSessionInboundEventMapper(executor),
			sessionMapper:                 NewSessionMapper(executor),
			sessionEventMapper:            NewSessionEventMapper(executor),
			sessionThreadMapper:           NewSessionThreadMapper(executor),
		})
	})
}

// ListSessionEventsForActivation returns complete public history in insertion order.
func (tx ManagedAgentEventTx) ListSessionEventsForActivation(
	ctx context.Context,
	session Session,
) ([]SessionEvent, error) {
	rows, err := tx.sessionEventMapper.ListSessionEventsForActivation(
		ctx,
		session.OrganizationUUID,
		session.WorkspaceUUID,
		session.UUID,
	)
	if err != nil {
		return nil, err
	}
	events := make([]SessionEvent, len(rows))
	for i, row := range rows {
		events[i] = row.event()
	}
	return events, nil
}

// AppendSessionEvents requires the owning Session to have been locked first.
func (tx ManagedAgentEventTx) AppendSessionEvents(ctx context.Context, session Session, events []SessionEvent, outcomeEvaluations json.RawMessage) ([]SessionEvent, error) {
	if session.ArchivedAt != nil {
		return nil, ErrInvalidState
	}
	created, err := insertSessionEventsTx(ctx, tx.executor, session, events, false, nil)
	if err != nil {
		return nil, err
	}
	if len(outcomeEvaluations) > 0 {
		_, err = tx.sessionMapper.SetOutcomeEvaluations(ctx, session.WorkspaceUUID, session.ExternalID, agentJSONArg(outcomeEvaluations))
		if err != nil {
			return nil, mapNoRows(err)
		}
	}
	return created, nil
}

// LockLatestCodeSession serializes active delivery with worker state changes.
// The caller must hold the owning Session lock, as activation does.
func (tx ManagedAgentEventTx) LockLatestCodeSession(ctx context.Context, session Session) (CodeSession, bool, error) {
	row, found, err := tx.codeSessionMapper.LockLatestForSession(ctx, session.WorkspaceUUID, session.UUID)
	return row.session(), found, err
}

// GetSessionCodeSession reads the source worker in the caller's locked Session
// transaction, rather than substituting a newer runtime for its pending actions.
func (tx ManagedAgentEventTx) GetSessionCodeSession(ctx context.Context, session Session, codeSessionID string) (CodeSession, bool, error) {
	row, found, err := tx.codeSessionMapper.FindForSession(ctx, session.WorkspaceUUID, session.UUID, codeSessionID)
	return row.session(), found, err
}

func (tx ManagedAgentEventTx) ClearWorkerMetadata(ctx context.Context, codeSession CodeSession, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	affected, err := tx.codeSessionMapper.ClearWorkerMetadata(ctx, codeSession.WorkspaceUUID, codeSession.UUID, keys)
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (tx ManagedAgentEventTx) MergeWorkerMetadata(ctx context.Context, codeSession CodeSession, metadata json.RawMessage) error {
	affected, err := tx.codeSessionMapper.MergeWorkerMetadata(ctx, codeSession.WorkspaceUUID, codeSession.UUID, metadata)
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (tx ManagedAgentEventTx) LockPublicEventWorker(ctx context.Context, session Session, worker SessionEventWorker) (CodeSession, error) {
	row, err := tx.codeSessionMapper.LockPublicEventWorker(ctx, session.WorkspaceUUID, session.UUID, worker.CodeSessionUUID)
	if err != nil {
		return CodeSession{}, mapNoRows(err)
	}
	if row.CurrentWorkerEpoch != worker.Epoch {
		return CodeSession{}, ErrWorkerEpochMismatch
	}
	return row.session(), nil
}

func (tx ManagedAgentEventTx) AppendSessionEventsIfAbsent(ctx context.Context, session Session, events []SessionEvent, ignoredPayloadFields []string) ([]SessionEvent, error) {
	if session.ArchivedAt != nil {
		return nil, ErrInvalidState
	}
	return insertSessionEventsTx(ctx, tx.executor, session, events, true, ignoredPayloadFields)
}

// SetSessionUsage replaces the snapshot in the owning Session's event transaction.
func (tx ManagedAgentEventTx) SetSessionUsage(ctx context.Context, session Session, usage json.RawMessage) error {
	affected, err := tx.sessionMapper.SetUsage(ctx, session.WorkspaceUUID, session.UUID, agentJSONArg(usage))
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (tx ManagedAgentEventTx) GetSessionThread(ctx context.Context, workspaceUUID, sessionExternalID, threadExternalID string) (SessionThread, error) {
	row, err := tx.sessionThreadMapper.FindByExternalID(ctx, workspaceUUID, sessionExternalID, threadExternalID)
	return row.thread(), mapNoRows(err)
}

func (tx ManagedAgentEventTx) GetPrimarySessionThread(ctx context.Context, workspaceUUID, sessionExternalID string) (SessionThread, bool, error) {
	row, err := tx.sessionThreadMapper.FindPrimary(ctx, workspaceUUID, sessionExternalID)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionThread{}, false, nil
	}
	return row.thread(), err == nil, err
}

func (tx ManagedAgentEventTx) CreateSessionThreadIfAbsent(ctx context.Context, thread SessionThread) (SessionThread, error) {
	return createSessionThreadIfAbsent(ctx, tx.sessionThreadMapper, thread)
}

func (tx ManagedAgentEventTx) ListSessionEventsPage(ctx context.Context, params ListSessionEventsPageParams) ([]SessionEvent, bool, error) {
	return listSessionEventsPage(ctx, tx.sessionEventMapper, params)
}

func (tx ManagedAgentEventTx) GetSessionEvent(ctx context.Context, session Session, eventID string) (SessionEvent, error) {
	row, err := tx.sessionEventMapper.FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, eventID)
	return row.event(), mapNoRows(err)
}

func (tx ManagedAgentEventTx) ListSessionThreads(ctx context.Context, session Session) ([]SessionThread, error) {
	rows, err := tx.sessionThreadMapper.List(ctx, session.WorkspaceUUID, session.ExternalID)
	return sessionThreadsFromRows(rows), err
}

// ToolUseOwnerThreadIDs resolves already accepted tool uses in this transaction.
// A primary blocking projection carries its child owner in the public payload.
func (tx ManagedAgentEventTx) ToolUseOwnerThreadIDs(ctx context.Context, session Session, toolUseID string) ([]string, error) {
	return tx.sessionEventMapper.ToolUseOwnerThreadIDs(ctx, session.WorkspaceUUID, session.ExternalID,
		[]string{"agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use"}, toolUseID)
}

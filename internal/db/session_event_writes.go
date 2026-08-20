package db

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"

	"github.com/superduck-ai/yourbatis"
)

// SessionEventWriteTx exposes the Session-scoped operations that must share
// the same row lock when resolving mounted files and appending public events.
type SessionEventWriteTx struct {
	sessionMapper         SessionMapper
	sessionEventMapper    SessionEventMapper
	sessionResourceMapper SessionResourceMapper
	sessionThreadMapper   SessionThreadMapper
}

// WithSessionEventWriteTx locks the Session before the callback resolves file
// bindings or inserts events, so resource deletion cannot race the write.
func (d *DB) WithSessionEventWriteTx(
	ctx context.Context,
	workspaceUUID string,
	sessionExternalID string,
	fn func(SessionEventWriteTx, Session) error,
) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		tx := SessionEventWriteTx{
			sessionMapper:         NewSessionMapper(executor),
			sessionEventMapper:    NewSessionEventMapper(executor),
			sessionResourceMapper: NewSessionResourceMapper(executor),
			sessionThreadMapper:   NewSessionThreadMapper(executor),
		}
		row, err := tx.sessionMapper.LockForMutation(ctx, workspaceUUID, sessionExternalID)
		if err != nil {
			return mapNoRows(err)
		}
		session := row.session()
		if session.ArchivedAt != nil {
			return ErrInvalidState
		}
		return fn(tx, session)
	})
}

// ListFileBindings returns active mounted File resources in stable creation
// order. When a file is mounted more than once, callers can select the first.
func (tx SessionEventWriteTx) ListFileBindings(
	ctx context.Context,
	session Session,
) ([]sessioncontract.EventFileBinding, error) {
	rows, err := tx.sessionResourceMapper.ListEventFileBindings(
		ctx,
		session.WorkspaceUUID,
		session.ExternalID,
	)
	if err != nil {
		return nil, err
	}
	return sessionEventFileBindingsFromRows(rows), nil
}

func sessionEventFileBindingsFromRows(rows []sessionEventFileBindingRow) []sessioncontract.EventFileBinding {
	bindings := make([]sessioncontract.EventFileBinding, len(rows))
	for index, row := range rows {
		bindings[index] = sessioncontract.EventFileBinding{
			FileID:   row.FileExternalID,
			Path:     row.Path,
			MimeType: row.MimeType,
		}
	}
	return bindings
}

// AppendEvents persists a validated batch and its optional outcome update.
func (tx SessionEventWriteTx) AppendEvents(
	ctx context.Context,
	session Session,
	events []SessionEvent,
	outcomeEvaluations json.RawMessage,
) ([]SessionEvent, error) {
	created, err := insertSessionEventsTxWithMappers(
		ctx,
		tx.sessionThreadMapper,
		tx.sessionEventMapper,
		session,
		events,
		false,
	)
	if err != nil || len(outcomeEvaluations) == 0 {
		return created, err
	}
	_, err = tx.sessionMapper.SetOutcomeEvaluations(
		ctx,
		session.WorkspaceUUID,
		session.ExternalID,
		agentJSONArg(outcomeEvaluations),
	)
	return created, mapNoRows(err)
}

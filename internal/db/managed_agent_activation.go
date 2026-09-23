package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

// ManagedAgentActivationTx exposes the resource-scoped SQL operations used by
// the code-session service while it publishes and activates startup events.
type ManagedAgentActivationTx struct {
	codeSessionMapper  CodeSessionMapper
	sessionMapper      SessionMapper
	sessionEventMapper SessionEventMapper
}

// WithManagedAgentActivationTx owns the database transaction lifecycle while
// leaving activation ordering and business decisions to the code-session service.
func (d *DB) WithManagedAgentActivationTx(
	ctx context.Context,
	fn func(ManagedAgentActivationTx) error,
) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return fn(ManagedAgentActivationTx{
			codeSessionMapper:  NewCodeSessionMapper(executor),
			sessionMapper:      NewSessionMapper(executor),
			sessionEventMapper: NewSessionEventMapper(executor),
		})
	})
}

// WithLockedActiveCodeSession serializes direct JetStream publication with
// other Code Session lifecycle changes. The callback runs while the row lock is
// held, so termination cannot purge the subject between the status check and
// PubAck.
func (d *DB) WithLockedActiveCodeSession(
	ctx context.Context,
	codeSessionExternalID string,
	fn func(CodeSession) error,
) error {
	return d.withLockedCodeSession(ctx, codeSessionExternalID, func(_ yourbatis.Executor, row codeSessionRow) error {
		if row.Status != "active" {
			return ErrInvalidState
		}
		return fn(row.session())
	})
}

// ListSessionEventsForActivation returns complete public history in insertion order.
func (tx ManagedAgentActivationTx) ListSessionEventsForActivation(
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

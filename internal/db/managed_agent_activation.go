package db

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"

	"github.com/superduck-ai/yourbatis"
)

// ManagedAgentActivationTx exposes the resource-scoped SQL operations used by
// the code-session service to atomically hand off startup events.
type ManagedAgentActivationTx struct {
	codeSessionMapper             CodeSessionMapper
	codeSessionInboundEventMapper CodeSessionInboundEventMapper
	sessionMapper                 SessionMapper
	sessionEventMapper            SessionEventMapper
	sessionResourceMapper         SessionResourceMapper
}

// WithManagedAgentActivationTx owns the database transaction lifecycle while
// leaving activation ordering and business decisions to the code-session service.
func (d *DB) WithManagedAgentActivationTx(
	ctx context.Context,
	fn func(ManagedAgentActivationTx) error,
) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return fn(ManagedAgentActivationTx{
			codeSessionMapper:             NewCodeSessionMapper(executor),
			codeSessionInboundEventMapper: NewCodeSessionInboundEventMapper(executor),
			sessionMapper:                 NewSessionMapper(executor),
			sessionEventMapper:            NewSessionEventMapper(executor),
			sessionResourceMapper:         NewSessionResourceMapper(executor),
		})
	})
}

// ListSessionEventFileBindings returns active file mounts while activation
// holds the owning Session row lock.
func (tx ManagedAgentActivationTx) ListSessionEventFileBindings(
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

package db

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// ManagedAgentActivationTx exposes the resource-scoped SQL operations used by
// the code-session service to atomically hand off startup events.
type ManagedAgentActivationTx struct {
	database *DB
	tx       *sqlx.Tx
}

// WithManagedAgentActivationTx owns the database transaction lifecycle while
// leaving activation ordering and business decisions to the code-session service.
func (d *DB) WithManagedAgentActivationTx(
	ctx context.Context,
	fn func(ManagedAgentActivationTx) error,
) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(ManagedAgentActivationTx{database: d, tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

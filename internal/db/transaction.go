package db

import (
	"context"
	"database/sql"

	"github.com/superduck-ai/yourbatis"
)

// Tx exposes business operations within a transaction owned by DB.Transaction.
// It must not escape the callback or be used asynchronously.
type Tx struct {
	executor *yourbatis.Tx
}

// Transaction commits only when fn succeeds; errors and panics roll back all writes.
func (d *DB) Transaction(ctx context.Context, fn func(*Tx) error) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return fn(&Tx{executor: executor.(*yourbatis.Tx)})
	})
}

// SQLTx shares the current transaction with integrations. Callers must not run
// application SQL through it, commit, roll back, or retain it after the callback.
func (tx *Tx) SQLTx() *sql.Tx {
	return tx.executor.SQLTx()
}

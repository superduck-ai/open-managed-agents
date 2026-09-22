package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

func (d *DB) EnvironmentTransaction(ctx context.Context, fn func(*yourbatis.Tx) error) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error { return fn(executor.(*yourbatis.Tx)) })
}
func (d *DB) CreateEnvironmentTx(ctx context.Context, tx *yourbatis.Tx, env Environment) (Environment, error) {
	row, err := NewEnvironmentMapper(tx).Insert(ctx, environmentWriteParamsFrom(env))
	if isUniqueViolation(err) {
		return Environment{}, ErrDuplicate
	}
	return row.environment(), err
}
func (d *DB) LockEnvironmentTx(ctx context.Context, tx *yourbatis.Tx, workspaceUUID, externalID string) (Environment, error) {
	m := NewEnvironmentMapper(tx)
	if _, err := m.LockUUIDByExternalID(ctx, workspaceUUID, externalID); err != nil {
		return Environment{}, mapNoRows(err)
	}
	row, err := m.FindByExternalID(ctx, workspaceUUID, externalID)
	return row.environment(), mapNoRows(err)
}
func (d *DB) UpdateEnvironmentTx(ctx context.Context, tx *yourbatis.Tx, env Environment) (Environment, error) {
	row, err := NewEnvironmentMapper(tx).UpdateByExternalID(ctx, environmentWriteParamsFrom(env))
	if isUniqueViolation(err) {
		return Environment{}, ErrDuplicate
	}
	return row.environment(), mapNoRows(err)
}

// Lock the owning environment before updating its build or the River checkpoint.
// Provider requests run outside this transaction.
func (d *DB) LockEnvironmentByUUIDTx(ctx context.Context, tx *yourbatis.Tx, workspaceUUID, environmentUUID string) (Environment, error) {
	row, err := NewEnvironmentMapper(tx).LockByUUID(ctx, workspaceUUID, environmentUUID)
	return row.environment(), mapNoRows(err)
}

func (d *DB) ResolveEnvironmentPrebuildTx(ctx context.Context, tx *yourbatis.Tx, workspaceUUID, environmentUUID string, jobID int64, template string) error {
	rows, err := NewEnvironmentMapper(tx).ResolvePrebuild(ctx, workspaceUUID, environmentUUID, jobID, template)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrInvalidState
	}
	return nil
}

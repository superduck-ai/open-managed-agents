package db

import (
	"context"
	"github.com/superduck-ai/yourbatis"
)

type PlatformAdminTx struct{ executor yourbatis.Executor }

func (d *DB) WithPlatformAdminTx(ctx context.Context, fn func(*PlatformAdminTx) error) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return fn(&PlatformAdminTx{executor: executor})
	})
}

func (d *DB) GetPlatformAdminAccount(ctx context.Context, accountUUID string) (PlatformAdminAccount, error) {
	row, err := NewPlatformAdminMapper(d.mapperDB).FindAccount(ctx, accountUUID)
	return row, mapNoRows(err)
}

func (d *DB) ListPlatformAdminAccounts(ctx context.Context, query PlatformAdminPageQuery) ([]PlatformAdminAccount, error) {
	return NewPlatformAdminMapper(d.mapperDB).ListAccounts(ctx, query)
}

func (d *DB) ListPlatformAdmins(ctx context.Context) ([]PlatformAdminAccount, error) {
	return NewPlatformAdminMapper(d.mapperDB).ListAdmins(ctx)
}

func (tx *PlatformAdminTx) LockGrant(ctx context.Context, accountUUID string) (bool, error) {
	granted, err := NewPlatformAdminMapper(tx.executor).LockGrant(ctx, accountUUID)
	return granted, mapNoRows(err)
}

func (tx *PlatformAdminTx) Account(ctx context.Context, accountUUID string) (PlatformAdminAccount, error) {
	row, err := NewPlatformAdminMapper(tx.executor).FindAccount(ctx, accountUUID)
	return row, mapNoRows(err)
}

func (tx *PlatformAdminTx) LockAdministration(ctx context.Context) error {
	_, err := NewPlatformAdminMapper(tx.executor).LockAdministration(ctx)
	return err
}

func (tx *PlatformAdminTx) CountAdmins(ctx context.Context) (int64, error) {
	return NewPlatformAdminMapper(tx.executor).CountAdmins(ctx)
}

func (tx *PlatformAdminTx) SetGrant(ctx context.Context, input PlatformAdminGrant, granted bool) error {
	mapper := NewPlatformAdminMapper(tx.executor)
	if granted {
		return mapper.Grant(ctx, input)
	}
	return mapper.Revoke(ctx, input)
}

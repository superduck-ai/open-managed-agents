package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func (d *DB) RegisterDSHWorker(ctx context.Context, scope DSHScope) (time.Time, error) {
	expiresAt := time.Now().UTC().Add(20 * time.Second)
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHRegistration(ctx, executor, scope); err != nil {
			return err
		}
		mapper := NewDSHWorkerMapper(executor)
		rows, err := mapper.Register(ctx, scope, expiresAt)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkerEpochMismatch
		}
		_, err = mapper.EnsureState(ctx, scope)
		return err
	})
	return expiresAt, err
}

func (d *DB) HeartbeatDSHWorker(ctx context.Context, scope DSHScope) (time.Time, error) {
	expiresAt := time.Now().UTC().Add(20 * time.Second)
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHWorker(ctx, executor, scope); err != nil {
			return err
		}
		rows, err := NewDSHWorkerMapper(executor).Heartbeat(ctx, scope, expiresAt)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrWorkerEpochMismatch
		}
		return nil
	})
	return expiresAt, err
}

func (d *DB) GetDSHContextSnapshot(ctx context.Context, scope DSHScope) ([]byte, error) {
	snapshot, _, err := NewDSHWorkerMapper(d.mapperDB).ContextSnapshot(ctx, scope)
	return snapshot, err
}

func (d *DB) FreezeDSHContext(ctx context.Context, scope DSHScope, candidate []byte) ([]byte, error) {
	if len(candidate) == 0 {
		return nil, ErrInvalidState
	}
	var snapshot []byte
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHRegistration(ctx, executor, scope); err != nil {
			return err
		}
		mapper := NewDSHWorkerMapper(executor)
		if _, err := mapper.EnsureState(ctx, scope); err != nil {
			return err
		}
		if _, err := mapper.FreezeContext(ctx, scope, candidate); err != nil {
			return err
		}
		var err error
		snapshot, _, err = mapper.ContextSnapshot(ctx, scope)
		return err
	})
	return snapshot, err
}

func lockDSHRegistration(ctx context.Context, executor yourbatis.Executor, scope DSHScope) error {
	owner, found, err := NewDSHTranscriptMapper(executor).LockWorker(ctx, scope)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	if scope.Epoch <= 0 || owner.WorkerEpoch != scope.Epoch {
		return ErrWorkerEpochMismatch
	}
	return nil
}

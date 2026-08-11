package db

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errAdvisoryLockDatabaseUnavailable = errors.New("database is not configured")

// TryAcquireAdvisoryLock keeps a Yourbatis transaction open for the lifetime
// of the returned release function because PostgreSQL advisory locks are
// session-scoped.
func (d *DB) TryAcquireAdvisoryLock(ctx context.Context, lockID int64) (func(), bool, error) {
	if d == nil || d.mapperDB == nil {
		return nil, false, errAdvisoryLockDatabaseUnavailable
	}
	tx, err := d.mapperDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	acquired, err := NewAdvisoryLockMapper(tx).TryAcquire(ctx, lockID)
	if err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if !acquired {
		_ = tx.Rollback()
		return func() {}, false, nil
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			unlocked, unlockErr := NewAdvisoryLockMapper(tx).Release(unlockCtx, lockID)
			if unlockErr == nil && unlocked {
				_ = tx.Rollback()
				return
			}
			// Rollback ends the transaction and prevents a failed unlock from
			// returning a potentially locked session to the pool.
			_ = tx.Rollback()
		})
	}
	return release, true, nil
}

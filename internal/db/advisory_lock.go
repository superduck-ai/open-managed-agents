package db

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errAdvisoryLockDatabaseUnavailable = errors.New("database is not configured")

// TryAcquireAdvisoryLock keeps the sqlx connection pinned for the lifetime of
// the returned release function because PostgreSQL advisory locks are
// session-scoped.
func (d *DB) TryAcquireAdvisoryLock(ctx context.Context, lockID int64) (func(), bool, error) {
	if d == nil || d.sql == nil {
		return nil, false, errAdvisoryLockDatabaseUnavailable
	}
	connection, err := d.sql.Connx(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := namedGetContext(ctx, connection, &acquired, `select pg_try_advisory_lock(:lock_id)`, map[string]any{
		"lock_id": lockID,
	}); err != nil {
		_ = connection.Close()
		return nil, false, err
	}
	if !acquired {
		_ = connection.Close()
		return func() {}, false, nil
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var unlocked bool
			unlockErr := namedGetContext(unlockCtx, connection, &unlocked, `select pg_advisory_unlock(:lock_id)`, map[string]any{
				"lock_id": lockID,
			})
			if unlockErr != nil || !unlocked {
				// Closing the pinned connection prevents a failed unlock from
				// returning a potentially locked session to the pool.
				_ = connection.Raw(func(driverConn any) error {
					if closer, ok := driverConn.(interface{ Close() error }); ok {
						return closer.Close()
					}
					return nil
				})
				_ = connection.Close()
				return
			}
			_ = connection.Close()
		})
	}
	return release, true, nil
}

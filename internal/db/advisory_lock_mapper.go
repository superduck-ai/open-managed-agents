package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper AdvisoryLockMapper -sql ./advisory_lock_mapper.xml -out ./advisory_lock_mapper.sqlmap.gen.go -dialect postgres

type AdvisoryLockMapper interface {
	TryAcquire(ctx context.Context, lockID int64) (bool, error)
	Release(ctx context.Context, lockID int64) (bool, error)
}

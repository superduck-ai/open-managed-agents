package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DSHWorkerMapper -sql ./dsh_worker_mapper.xml -out ./dsh_worker_mapper.sqlmap.gen.go -dialect postgres

type DSHWorkerMapper interface {
	ContextSnapshot(ctx context.Context, scope DSHScope) ([]byte, bool, error)
	FreezeContext(ctx context.Context, scope DSHScope, snapshot []byte) (int64, error)
	Register(ctx context.Context, scope DSHScope, expiresAt time.Time) (int64, error)
	Heartbeat(ctx context.Context, scope DSHScope, expiresAt time.Time) (int64, error)
	EnsureState(ctx context.Context, scope DSHScope) (int64, error)
}

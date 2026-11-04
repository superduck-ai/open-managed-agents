package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DSHWorkerMapper -sql ./dsh_worker_mapper.xml -out ./dsh_worker_mapper.sqlmap.gen.go -dialect postgres

type dshWorkerStateRow struct {
	Model                *string `db:"model"`
	TraceTurns           []byte  `db:"trace_turns"`
	ModelControlSequence *string `db:"model_control_sequence"`
	ModelControlInputID  *string `db:"model_control_input_id"`
}

type DSHWorkerMapper interface {
	Register(ctx context.Context, scope DSHScope, expiresAt time.Time) (int64, error)
	Heartbeat(ctx context.Context, scope DSHScope, expiresAt time.Time) (int64, error)
	EnsureState(ctx context.Context, scope DSHScope) (int64, error)
	State(ctx context.Context, scope DSHScope) (dshWorkerStateRow, bool, error)
	Patch(ctx context.Context, scope DSHScope, patch DSHWorkerPatch) (int64, error)
}

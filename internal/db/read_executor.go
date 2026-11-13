package db

import (
	"context"
	"github.com/superduck-ai/yourbatis"
)

type validationExecutorKey struct{}
type validationExecutor struct {
	database *DB
	executor yourbatis.Executor
}

func (d *DB) readExecutor(ctx context.Context) yourbatis.Executor {
	if bound, ok := ctx.Value(validationExecutorKey{}).(validationExecutor); ok && bound.database == d {
		return bound.executor
	}
	return d.mapperDB
}

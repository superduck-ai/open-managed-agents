package db

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/superduck-ai/yourbatis"
)

// sqlxTxExecutor is limited to legacy transactions that still combine sqlx
// statements with generated mappers. New mapper-owned transactions start from
// DB.mapperDB and receive the YourBatis transaction Executor directly.
type sqlxTxExecutor struct {
	database *sqlx.Tx
}

var _ yourbatis.Executor = sqlxTxExecutor{}

func newSQLXTxExecutor(database *sqlx.Tx) yourbatis.Executor {
	return sqlxTxExecutor{database: database}
}

func (sqlxTxExecutor) Dialect() yourbatis.Dialect {
	return yourbatis.DialectPostgres
}

func (executor sqlxTxExecutor) Query(
	ctx context.Context,
	_ yourbatis.Statement,
	bound yourbatis.BoundSQL,
) (*sql.Rows, error) {
	return executor.database.QueryContext(ctx, bound.SQL, bound.Values()...)
}

func (executor sqlxTxExecutor) Exec(
	ctx context.Context,
	_ yourbatis.Statement,
	bound yourbatis.BoundSQL,
) (sql.Result, error) {
	return executor.database.ExecContext(ctx, bound.SQL, bound.Values()...)
}

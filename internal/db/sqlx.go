package db

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type sqlxNamedQueryer interface {
	sqlx.QueryerContext
	Rebind(string) string
}

type sqlxNamedExecer interface {
	sqlx.ExecerContext
	Rebind(string) string
}

func bindNamed(database interface{ Rebind(string) string }, query string, arguments map[string]any) (string, []any, error) {
	boundQuery, values, err := sqlx.Named(query, arguments)
	if err != nil {
		return "", nil, err
	}
	return database.Rebind(boundQuery), values, nil
}

func namedGetContext(ctx context.Context, database sqlxNamedQueryer, destination any, query string, arguments map[string]any) error {
	boundQuery, values, err := bindNamed(database, query, arguments)
	if err != nil {
		return err
	}
	return sqlx.GetContext(ctx, database, destination, boundQuery, values...)
}

func namedSelectContext(ctx context.Context, database sqlxNamedQueryer, destination any, query string, arguments map[string]any) error {
	boundQuery, values, err := bindNamed(database, query, arguments)
	if err != nil {
		return err
	}
	return sqlx.SelectContext(ctx, database, destination, boundQuery, values...)
}

func namedExecContext(ctx context.Context, database sqlxNamedExecer, query string, arguments map[string]any) (sql.Result, error) {
	boundQuery, values, err := bindNamed(database, query, arguments)
	if err != nil {
		return nil, err
	}
	return database.ExecContext(ctx, boundQuery, values...)
}

func namedExecRowsAffected(ctx context.Context, database sqlxNamedExecer, query string, arguments map[string]any) (int64, error) {
	result, err := namedExecContext(ctx, database, query, arguments)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

package db

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

func namedGetContext(ctx context.Context, database *sqlx.DB, destination any, query string, arguments map[string]any) error {
	boundQuery, values, err := database.BindNamed(query, arguments)
	if err != nil {
		return err
	}
	return database.GetContext(ctx, destination, boundQuery, values...)
}

func namedSelectContext(ctx context.Context, database *sqlx.DB, destination any, query string, arguments map[string]any) error {
	boundQuery, values, err := database.BindNamed(query, arguments)
	if err != nil {
		return err
	}
	return database.SelectContext(ctx, destination, boundQuery, values...)
}

func namedExecContext(ctx context.Context, database *sqlx.DB, query string, arguments map[string]any) (sql.Result, error) {
	boundQuery, values, err := database.BindNamed(query, arguments)
	if err != nil {
		return nil, err
	}
	return database.ExecContext(ctx, boundQuery, values...)
}

func namedExecRowsAffected(ctx context.Context, database *sqlx.DB, query string, arguments map[string]any) (int64, error) {
	result, err := namedExecContext(ctx, database, query, arguments)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

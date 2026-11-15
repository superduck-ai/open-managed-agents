package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

func runGooseMigrations(ctx context.Context, standardDB *sql.DB) error {
	migrationFS, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	sessionLocker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(5, 60))
	if err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		standardDB,
		migrationFS,
		goose.WithSessionLocker(sessionLocker),
		goose.WithDisableGlobalRegistry(true),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func (d *DB) schemaVersion(ctx context.Context) (int64, error) {
	var exists bool
	if err := d.SQLDB().QueryRowContext(ctx, "SELECT to_regclass('goose_db_version') IS NOT NULL").Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	var version int64
	err := d.SQLDB().QueryRowContext(ctx, "SELECT COALESCE(max(version_id), 0) FROM goose_db_version WHERE is_applied").Scan(&version)
	return version, err
}

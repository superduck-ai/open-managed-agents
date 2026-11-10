package db

import (
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type OAuthClientMigrationRecord struct {
	UUID                 string
	ExternalID           string
	OrganizationUUID     string
	WorkspaceUUID        string
	LegacyClientID       string
	ClientIDEnvelope     *secrets.Envelope
	ClientSecretEnvelope *secrets.Envelope
	SecretDocument       string
}

func (d *DB) WithOAuthMaintenanceLock(ctx context.Context, run func() error) error {
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 1))
	if err != nil {
		return err
	}
	conn, err := d.SQLDB().Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := locker.SessionLock(ctx, conn); err != nil {
		return err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := locker.SessionUnlock(unlockCtx, conn); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	return run()
}

func (d *DB) OAuthMigrationVersion(ctx context.Context) (int64, error) {
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

func (d *DB) MigrateOAuthSchemaTo(ctx context.Context, version int64) error {
	current, err := d.OAuthMigrationVersion(ctx)
	if err != nil {
		return err
	}
	if version < current || (version != 118 && version != 119 && version != 120 && version != 121) {
		return errors.New("invalid OAuth migration target")
	}
	migrationFS, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, d.SQLDB(), migrationFS, goose.WithDisableGlobalRegistry(true), goose.WithLogger(goose.NopLogger()))
	if err != nil {
		return err
	}
	_, err = provider.UpTo(ctx, version)
	return err
}

func (d *DB) ListOAuthClientMigrationPage(ctx context.Context, kind, after string, transitional, expanded bool) ([]OAuthClientMigrationRecord, error) {
	if kind != "platform_oauth_providers" && kind != "env_oauth_flows" {
		return nil, errors.New("invalid OAuth migration kind")
	}
	rows, err := NewOAuthClientMigrationMapper(d.mapperDB).ListPage(ctx, kind, after, transitional, expanded)
	if err != nil {
		return nil, err
	}
	records := make([]OAuthClientMigrationRecord, 0, len(rows))
	for _, row := range rows {
		id, err := decodeOAuthEnvelope(row.ClientIDEnvelope)
		if err != nil {
			return nil, err
		}
		secret, err := decodeOAuthEnvelope(row.ClientSecretEnvelope)
		if err != nil {
			return nil, err
		}
		records = append(records, OAuthClientMigrationRecord{UUID: row.UUID, ExternalID: row.ExternalID,
			OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID, LegacyClientID: row.LegacyClientID,
			ClientIDEnvelope: id, ClientSecretEnvelope: secret, SecretDocument: row.ClientSecretEnvelope.String})
	}
	return records, nil
}

func (d *DB) BackfillOAuthClientID(ctx context.Context, kind string, expected OAuthClientMigrationRecord, envelope secrets.Envelope) error {
	if kind != "platform_oauth_providers" && kind != "env_oauth_flows" {
		return errors.New("invalid OAuth migration kind")
	}
	if err := requireCompleteSecretEnvelope(&envelope); err != nil {
		return err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	affected, err := NewOAuthClientMigrationMapper(d.mapperDB).Backfill(ctx, kind, expected, string(encoded))
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrVersionConflict
	}
	return nil
}

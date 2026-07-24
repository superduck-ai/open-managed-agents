package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/user"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

var (
	ErrNotFound              = platform.ErrNotFound
	ErrInvalidState          = errors.New("invalid state")
	ErrPreconditionFailed    = errors.New("precondition failed")
	ErrDuplicate             = errors.New("duplicate")
	ErrVersionConflict       = errors.New("version conflict")
	ErrWorkerEpochMismatch   = errors.New("worker epoch mismatch")
	ErrWorkerNotRegistered   = errors.New("worker not registered")
	ErrWorkerLeaseExpired    = errors.New("worker lease expired")
	ErrStorageLimitExceeded  = errors.New("storage limit exceeded")
	ErrStorageUsageUnderflow = errors.New("storage usage underflow")
	ErrLimitExceeded         = errors.New("limit exceeded")
	ErrFileInUse             = errors.New("file is in use")
	ErrFileReferenceNotFound = errors.New("file reference not found")
)

type DB struct {
	Pool *pgxpool.Pool
	sql  *sqlx.DB
}

type APIKey struct {
	ID                     int64
	ExternalID             string
	OrganizationID         int64
	OrganizationExternalID string
	WorkspaceID            int64
	WorkspaceUUID          string
	WorkspaceExternalID    string
}

func Open(ctx context.Context, cfg config.Config) (*DB, error) {
	pool, err := openPool(ctx, cfg.Database.URL)
	if err == nil {
		return newDB(pool), nil
	}

	if bootstrapErr := EnsureDatabase(ctx, cfg.Database.URL); bootstrapErr != nil {
		return nil, fmt.Errorf("connect database: %w; bootstrap database: %v", err, bootstrapErr)
	}

	pool, err = openPool(ctx, cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("connect database after bootstrap: %w", err)
	}
	return newDB(pool), nil
}

func newDB(pool *pgxpool.Pool) *DB {
	// sqlx 只提供命名参数与结构体映射，物理连接仍统一由 pgxpool 管理。
	// OpenDBFromPool 会把 database/sql 的 MaxIdleConns 固定为 0，避免包装层长期占住
	// pgxpool 连接；最大连接数与连接寿命继续由上面的唯一 pgxpool 约束。
	standardDB := stdlib.OpenDBFromPool(pool)
	return &DB{
		Pool: pool,
		sql:  sqlx.NewDb(standardDB, "pgx"),
	}
}

func openPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func (d *DB) Close() {
	if d == nil {
		return
	}
	if d.sql != nil {
		_ = d.sql.Close()
	}
	if d.Pool != nil {
		d.Pool.Close()
	}
}

func EnsureDatabase(ctx context.Context, databaseURL string) error {
	var candidates []string
	for _, maintenanceDB := range []string{"postgres", "template1"} {
		if candidate, err := maintenanceURL(databaseURL, maintenanceDB); err == nil && !slices.Contains(candidates, candidate) {
			candidates = append(candidates, candidate)
		}
	}
	for _, candidate := range currentUserMaintenanceURLs(databaseURL) {
		if !slices.Contains(candidates, candidate) {
			candidates = append(candidates, candidate)
		}
	}

	var errs []string
	for _, candidate := range candidates {
		if err := ensureDatabaseWithMaintenanceConnection(ctx, databaseURL, candidate); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", redactPassword(candidate), err))
			continue
		}
		return nil
	}
	return fmt.Errorf("all database bootstrap connection attempts failed: %s", strings.Join(errs, "; "))
}

func ensureDatabaseWithMaintenanceConnection(ctx context.Context, databaseURL, maintenanceDatabaseURL string) error {
	target, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database URL: %w", err)
	}
	dbName := strings.TrimPrefix(target.Path, "/")
	if dbName == "" {
		return errors.New("database URL must include a database name")
	}
	role := target.User.Username()
	password, _ := target.User.Password()
	if role == "" {
		return errors.New("database URL must include a database user")
	}

	maintenancePool, err := openPool(ctx, maintenanceDatabaseURL)
	if err != nil {
		return fmt.Errorf("connect maintenance database: %w", err)
	}
	defer maintenancePool.Close()

	var roleExists bool
	if err := maintenancePool.QueryRow(ctx, "select exists(select 1 from pg_roles where rolname=$1)", role).Scan(&roleExists); err != nil {
		return fmt.Errorf("check role: %w", err)
	}
	if !roleExists {
		if _, err := maintenancePool.Exec(ctx, fmt.Sprintf("create role %s login password %s", quoteIdent(role), quoteLiteral(password))); err != nil {
			return fmt.Errorf("create role %s: %w", role, err)
		}
	} else if password != "" {
		if _, err := maintenancePool.Exec(ctx, fmt.Sprintf("alter role %s with password %s", quoteIdent(role), quoteLiteral(password))); err != nil {
			return fmt.Errorf("alter role %s password: %w", role, err)
		}
	}

	var dbExists bool
	if err := maintenancePool.QueryRow(ctx, "select exists(select 1 from pg_database where datname=$1)", dbName).Scan(&dbExists); err != nil {
		return fmt.Errorf("check database: %w", err)
	}
	if !dbExists {
		if _, err := maintenancePool.Exec(ctx, fmt.Sprintf("create database %s owner %s", quoteIdent(dbName), quoteIdent(role))); err != nil {
			return fmt.Errorf("create database %s: %w", dbName, err)
		}
	}
	return nil
}

func maintenanceURL(databaseURL, databaseName string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + databaseName
	return parsed.String(), nil
}

func currentUserMaintenanceURLs(databaseURL string) []string {
	current, err := user.Current()
	if err != nil || current.Username == "" {
		return nil
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return nil
	}
	parsed.User = url.User(current.Username)
	urls := make([]string, 0, 2)
	for _, dbName := range []string{"postgres", "template1"} {
		clone := *parsed
		clone.Path = "/" + dbName
		urls = append(urls, clone.String())
	}
	return urls
}

func redactPassword(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	username := parsed.User.Username()
	if _, hasPassword := parsed.User.Password(); hasPassword {
		parsed.User = url.UserPassword(username, "xxxxx")
	}
	return parsed.String()
}

func (d *DB) idColumnDataType(ctx context.Context, table string) (string, error) {
	var dataType string
	err := d.Pool.QueryRow(ctx, `
		select coalesce((
			select data_type
			from information_schema.columns
			where table_schema = current_schema()
				and table_name = $1
				and column_name = 'id'
		), '')
	`, table).Scan(&dataType)
	return dataType, err
}

func (d *DB) migrateLegacyTextIDSchema(ctx context.Context) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tables := []string{"jobs", "files", "api_keys", "workspaces", "organizations"}
	for _, table := range tables {
		legacy := table + "_legacy_text_ids"
		var exists bool
		if err := tx.QueryRow(ctx, "select to_regclass($1) is not null", legacy).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("legacy table %s already exists; refusing to overwrite it", legacy)
		}
	}

	for _, table := range tables {
		legacy := table + "_legacy_text_ids"
		if _, err := tx.Exec(ctx, fmt.Sprintf("alter table if exists %s rename to %s", quoteIdent(table), quoteIdent(legacy))); err != nil {
			return fmt.Errorf("rename %s to %s: %w", table, legacy, err)
		}
	}

	if _, err := tx.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create bigint-id schema: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		insert into organizations (external_id, name, created_at)
		select id, name, created_at
		from organizations_legacy_text_ids
		on conflict (external_id) do nothing
	`); err != nil {
		return fmt.Errorf("copy organizations: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into workspaces (external_id, organization_id, name, created_at)
		select w.id, o.id, w.name, w.created_at
		from workspaces_legacy_text_ids w
		join organizations o on o.external_id = w.organization_id
		on conflict (external_id) do nothing
	`); err != nil {
		return fmt.Errorf("copy workspaces: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into api_keys (external_id, workspace_id, key_hash, status, created_at)
		select ak.id, w.id, ak.key_hash, ak.status, ak.created_at
		from api_keys_legacy_text_ids ak
		join workspaces w on w.external_id = ak.workspace_id
		on conflict (external_id) do nothing
	`); err != nil {
		return fmt.Errorf("copy api_keys: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into files (
			external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at, deleted_at
		)
		select f.id, w.id, f.filename, f.mime_type, f.size_bytes, f.sha256,
			f.s3_bucket, f.s3_key, f.downloadable, f.scope_type, f.scope_id, ak.id, f.created_at, f.deleted_at
		from files_legacy_text_ids f
		join workspaces w on w.external_id = f.workspace_id
		join api_keys ak on ak.external_id = f.created_by_api_key_id
		on conflict (external_id) do nothing
	`); err != nil {
		return fmt.Errorf("copy files: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into jobs (
			external_id, workspace_id, type, status, payload, attempts,
			locked_by, locked_until, run_after, created_at, updated_at
		)
		select j.id, w.id, j.type, j.status, j.payload, j.attempts,
			j.locked_by, j.locked_until, j.run_after, j.created_at, j.updated_at
		from jobs_legacy_text_ids j
		join workspaces w on w.external_id = j.workspace_id
		on conflict (external_id) do nothing
	`); err != nil {
		return fmt.Errorf("copy jobs: %w", err)
	}

	return tx.Commit(ctx)
}

func (d *DB) Migrate(ctx context.Context) error {
	dataType, err := d.idColumnDataType(ctx, "organizations")
	if err != nil {
		return err
	}
	if dataType == "text" {
		if err := d.migrateLegacyTextIDSchema(ctx); err != nil {
			return err
		}
	}
	if err := d.runGooseMigrations(ctx); err != nil {
		return err
	}
	return d.DropForeignKeyConstraints(ctx)
}

func (d *DB) DropForeignKeyConstraints(ctx context.Context) error {
	rows, err := d.Pool.Query(ctx, `
		select cls.relname, con.conname
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where con.contype = 'f'
			and ns.oid = current_schema()::regnamespace
		order by cls.relname, con.conname
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type foreignKey struct {
		table string
		name  string
	}
	var constraints []foreignKey
	for rows.Next() {
		var fk foreignKey
		if err := rows.Scan(&fk.table, &fk.name); err != nil {
			return err
		}
		constraints = append(constraints, fk)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, fk := range constraints {
		if _, err := d.Pool.Exec(ctx, fmt.Sprintf("alter table %s drop constraint %s", quoteIdent(fk.table), quoteIdent(fk.name))); err != nil {
			return fmt.Errorf("drop foreign key %s on %s: %w", fk.name, fk.table, err)
		}
	}
	return nil
}

func (d *DB) Seed(ctx context.Context, seedAPIKeys []config.SeedAPIKey) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var organizationID int64
	if err := tx.QueryRow(ctx, `
		insert into organizations (external_id, name)
		values ($1, $2)
		on conflict (external_id) do update set name = excluded.name
		returning id
	`, "org_default", "default").Scan(&organizationID); err != nil {
		return err
	}
	var workspaceID int64
	if err := tx.QueryRow(ctx, `
		insert into workspaces (external_id, organization_id, name)
		values ($1, $2, $3)
		on conflict (external_id) do update set
			organization_id = excluded.organization_id,
			name = excluded.name
		returning id
	`, "workspace_default", organizationID, "default").Scan(&workspaceID); err != nil {
		return err
	}
	var userID int64
	if err := tx.QueryRow(ctx, `
		insert into users (external_id, organization_id, email, name, role)
		values ($1, $2, $3, $4, 'admin')
		on conflict (external_id) do update set
			organization_id = excluded.organization_id,
			email = excluded.email,
			name = excluded.name,
			role = excluded.role,
			deleted_at = null,
			updated_at = now()
		returning id
	`, "user_default", organizationID, "admin@example.local", "Local Admin").Scan(&userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into workspace_members (
			external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role
		)
		values ($1, $2, $3, $4, $5, $6, 'workspace_admin')
		on conflict (external_id) do update set
			organization_id = excluded.organization_id,
			workspace_id = excluded.workspace_id,
			workspace_external_id = excluded.workspace_external_id,
			user_id = excluded.user_id,
			user_external_id = excluded.user_external_id,
			workspace_role = excluded.workspace_role,
			deleted_at = null,
			updated_at = now()
	`, "wmem_default", organizationID, workspaceID, "workspace_default", userID, "user_default"); err != nil {
		return err
	}

	for _, key := range seedAPIKeys {
		if strings.TrimSpace(key.ExternalID) == "" || key.Key == "" {
			return errors.New("seed api keys must include external_id and key")
		}
		if _, err := tx.Exec(ctx, `
			insert into api_keys (external_id, workspace_id, key_hash, status, created_by_user_id, name, partial_key_hint)
			values ($1, $2, $3, 'active', $4, $5, $6)
			on conflict (external_id) do update set
				workspace_id = excluded.workspace_id,
				key_hash = excluded.key_hash,
				status = 'active',
				created_by_user_id = excluded.created_by_user_id,
				name = excluded.name,
				partial_key_hint = excluded.partial_key_hint,
				updated_at = now()
		`, key.ExternalID, workspaceID, auth.HashAPIKey(key.Key), userID, key.ExternalID, partialAPIKeyHint(key.Key)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (d *DB) GetAPIKey(ctx context.Context, keyHash string) (APIKey, error) {
	var key APIKey
	err := d.Pool.QueryRow(ctx, `
		select ak.id, ak.external_id, o.id, o.external_id, w.id, w.uuid::text, w.external_id
		from api_keys ak
		join workspaces w on w.id = ak.workspace_id
		join organizations o on o.id = w.organization_id
		where ak.key_hash = $1
			and ak.status = 'active'
			and (ak.expires_at is null or ak.expires_at > now())
	`, keyHash).Scan(&key.ID, &key.ExternalID, &key.OrganizationID, &key.OrganizationExternalID, &key.WorkspaceID, &key.WorkspaceUUID, &key.WorkspaceExternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	return key, err
}

func partialAPIKeyHint(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

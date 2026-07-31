package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os/user"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

var (
	ErrNotFound                      = platform.ErrNotFound
	ErrInvalidState                  = errors.New("invalid state")
	ErrPreconditionFailed            = errors.New("precondition failed")
	ErrDuplicate                     = errors.New("duplicate")
	ErrVersionConflict               = errors.New("version conflict")
	ErrWorkerEpochMismatch           = errors.New("worker epoch mismatch")
	ErrWorkerNotRegistered           = errors.New("worker not registered")
	ErrWorkerLeaseExpired            = errors.New("worker lease expired")
	ErrStorageLimitExceeded          = errors.New("storage limit exceeded")
	ErrStorageUsageUnderflow         = errors.New("storage usage underflow")
	ErrLimitExceeded                 = errors.New("limit exceeded")
	ErrFileInUse                     = errors.New("file is in use")
	ErrFileReferenceNotFound         = errors.New("file reference not found")
	ErrSessionStartupMessageConflict = errors.New("session startup message conflict")
)

type DB struct {
	Pool *pgxpool.Pool
	sql  *sqlx.DB
}

type APIKey struct {
	UUID                uuid.UUID
	ExternalID          string
	OrganizationUUID    uuid.UUID
	WorkspaceUUID       uuid.UUID
	WorkspaceExternalID string
}

type foreignKeyRow struct {
	Table string `db:"table_name"`
	Name  string `db:"constraint_name"`
}

type apiKeyRow struct {
	UUID                uuid.UUID `db:"uuid"`
	ExternalID          string    `db:"external_id"`
	OrganizationUUID    uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID       uuid.UUID `db:"workspace_uuid"`
	WorkspaceExternalID string    `db:"workspace_external_id"`
}

const (
	maintenanceRoleExistsQuery     = `select exists(select 1 from pg_roles where rolname = :role)`
	maintenanceDatabaseExistsQuery = `
		select exists(select 1 from pg_database where datname = :database_name)
	`
	idColumnDataTypeQuery = `
		select coalesce((
			select data_type
			from information_schema.columns
			where table_schema = current_schema()
				and table_name = :table_name
				and column_name = 'id'
		), '')
	`
	legacyTableExistsQuery = `select to_regclass(:table_name) is not null`
	seedOrganizationQuery  = `
		with existing as (
			select o.uuid
			from organizations o
			join workspaces w on w.organization_uuid = o.uuid
			where w.external_id = :workspace_external_id
			limit 1
		),
		updated as (
			update organizations o
			set name = :name,
				updated_at = now()
			from existing
			where o.uuid = existing.uuid
			returning o.uuid
		),
		inserted as (
			insert into organizations (name)
			select :name
			where not exists (select 1 from existing)
			returning uuid
		)
		select uuid from updated
		union all
		select uuid from inserted
		limit 1
	`
	seedOrganizationLockQuery = `select pg_advisory_xact_lock(704611533427849228)`
	seedWorkspaceQuery        = `
		insert into workspaces (external_id, organization_uuid, name)
		values (:external_id, :organization_uuid, :name)
		on conflict (external_id) do update set
			organization_uuid = excluded.organization_uuid,
			name = excluded.name
		returning uuid
	`
	seedUserQuery = `
		insert into users (external_id, organization_uuid, email, name, role)
		values (:external_id, :organization_uuid, :email, :name, 'admin')
		on conflict (external_id) do update set
			organization_uuid = excluded.organization_uuid,
			email = excluded.email,
			name = excluded.name,
			role = excluded.role,
			deleted_at = null,
			updated_at = now()
		returning uuid
	`
	seedWorkspaceMemberQuery = `
		insert into workspace_members (
			external_id, organization_uuid, workspace_uuid, workspace_external_id,
			user_uuid, user_external_id, workspace_role
		)
		values (
			:external_id, :organization_uuid, :workspace_uuid,
			:workspace_external_id, :user_uuid, :user_external_id, 'workspace_admin'
		)
		on conflict (external_id) do update set
			organization_uuid = excluded.organization_uuid,
			workspace_uuid = excluded.workspace_uuid,
			workspace_external_id = excluded.workspace_external_id,
			user_uuid = excluded.user_uuid,
			user_external_id = excluded.user_external_id,
			workspace_role = excluded.workspace_role,
			deleted_at = null,
			updated_at = now()
	`
	seedAPIKeyQuery = `
		insert into api_keys (
			external_id, workspace_uuid, key_hash, status, created_by_user_uuid, name, partial_key_hint
		)
		values (
			:external_id, :workspace_uuid, :key_hash, 'active',
			:created_by_user_uuid, :name, :partial_key_hint
		)
		on conflict (external_id) do update set
			workspace_uuid = excluded.workspace_uuid,
			key_hash = excluded.key_hash,
			status = 'active',
			created_by_user_uuid = excluded.created_by_user_uuid,
			name = excluded.name,
			partial_key_hint = excluded.partial_key_hint,
			updated_at = now()
	`
	getAPIKeyQuery = `
		select ak.uuid, ak.external_id,
			w.organization_uuid,
			w.uuid as workspace_uuid,
			w.external_id as workspace_external_id
		from api_keys ak
		join workspaces w on w.uuid = ak.workspace_uuid
		where ak.key_hash = :key_hash
			and ak.status = 'active'
			and (ak.expires_at is null or ak.expires_at > now())
	`
)

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
	return &DB{
		Pool: pool,
		sql:  newSQLXDB(pool),
	}
}

func newSQLXDB(pool *pgxpool.Pool) *sqlx.DB {
	// sqlx 只提供命名参数与结构体映射，物理连接仍统一由 pgxpool 管理。
	// OpenDBFromPool 会把 database/sql 的 MaxIdleConns 固定为 0，避免包装层长期占住
	// pgxpool 连接；最大连接数与连接寿命继续由上面的唯一 pgxpool 约束。
	standardDB := stdlib.OpenDBFromPool(pool)
	return sqlx.NewDb(standardDB, "pgx")
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
	maintenanceSQL := newSQLXDB(maintenancePool)
	defer maintenanceSQL.Close()

	var roleExists bool
	if err := namedGetContext(ctx, maintenanceSQL, &roleExists,
		maintenanceRoleExistsQuery,
		map[string]any{"role": role},
	); err != nil {
		return fmt.Errorf("check role: %w", err)
	}
	if !roleExists {
		if _, err := maintenanceSQL.ExecContext(ctx, fmt.Sprintf("create role %s login password %s", quoteIdent(role), quoteLiteral(password))); err != nil {
			return fmt.Errorf("create role %s: %w", role, err)
		}
	} else if password != "" {
		if _, err := maintenanceSQL.ExecContext(ctx, fmt.Sprintf("alter role %s with password %s", quoteIdent(role), quoteLiteral(password))); err != nil {
			return fmt.Errorf("alter role %s password: %w", role, err)
		}
	}

	var dbExists bool
	if err := namedGetContext(ctx, maintenanceSQL, &dbExists,
		maintenanceDatabaseExistsQuery,
		map[string]any{"database_name": dbName},
	); err != nil {
		return fmt.Errorf("check database: %w", err)
	}
	if !dbExists {
		if _, err := maintenanceSQL.ExecContext(ctx, fmt.Sprintf("create database %s owner %s", quoteIdent(dbName), quoteIdent(role))); err != nil {
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
	err := namedGetContext(ctx, d.sql, &dataType, idColumnDataTypeQuery, map[string]any{"table_name": table})
	return dataType, err
}

func (d *DB) migrateLegacyTextIDSchema(ctx context.Context) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables := []string{"jobs", "files", "api_keys", "workspaces", "organizations"}
	for _, table := range tables {
		legacy := table + "_legacy_text_ids"
		var exists bool
		if err := namedGetContext(ctx, tx, &exists,
			legacyTableExistsQuery,
			map[string]any{"table_name": legacy},
		); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("legacy table %s already exists; refusing to overwrite it", legacy)
		}
	}

	for _, table := range tables {
		legacy := table + "_legacy_text_ids"
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("alter table if exists %s rename to %s", quoteIdent(table), quoteIdent(legacy))); err != nil {
			return fmt.Errorf("rename %s to %s: %w", table, legacy, err)
		}
	}

	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create bigint-id schema: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		insert into organizations (external_id, name, created_at)
		select id, name, created_at
		from organizations_legacy_text_ids
		on conflict (external_id) do nothing
	`); err != nil {
		return fmt.Errorf("copy organizations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		insert into workspaces (external_id, organization_id, name, created_at)
		select w.id, o.id, w.name, w.created_at
		from workspaces_legacy_text_ids w
		join organizations o on o.external_id = w.organization_id
		on conflict (external_id) do nothing
	`); err != nil {
		return fmt.Errorf("copy workspaces: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		insert into api_keys (external_id, workspace_id, key_hash, status, created_at)
		select ak.id, w.id, ak.key_hash, ak.status, ak.created_at
		from api_keys_legacy_text_ids ak
		join workspaces w on w.external_id = ak.workspace_id
		on conflict (external_id) do nothing
	`); err != nil {
		return fmt.Errorf("copy api_keys: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
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
	if _, err := tx.ExecContext(ctx, `
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

	return tx.Commit()
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
	var constraints []foreignKeyRow
	if err := d.sql.SelectContext(ctx, &constraints, `
		select cls.relname as table_name, con.conname as constraint_name
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where con.contype = 'f'
			and ns.oid = CAST(current_schema() AS regnamespace)
		order by cls.relname, con.conname
	`); err != nil {
		return err
	}

	for _, fk := range constraints {
		if _, err := d.sql.ExecContext(ctx, fmt.Sprintf("alter table %s drop constraint %s", quoteIdent(fk.Table), quoteIdent(fk.Name))); err != nil {
			return fmt.Errorf("drop foreign key %s on %s: %w", fk.Name, fk.Table, err)
		}
	}
	return nil
}

func (d *DB) Seed(ctx context.Context, seedAPIKeys []config.SeedAPIKey) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, seedOrganizationLockQuery); err != nil {
		return err
	}
	var organizationUUID uuid.UUID
	if err := namedGetContext(ctx, tx, &organizationUUID, seedOrganizationQuery, map[string]any{
		"workspace_external_id": "workspace_default",
		"name":                  "default",
	}); err != nil {
		return err
	}
	var workspaceUUID uuid.UUID
	if err := namedGetContext(ctx, tx, &workspaceUUID, seedWorkspaceQuery, map[string]any{
		"external_id":       "workspace_default",
		"organization_uuid": organizationUUID,
		"name":              "default",
	}); err != nil {
		return err
	}
	var userUUID uuid.UUID
	if err := namedGetContext(ctx, tx, &userUUID, seedUserQuery, map[string]any{
		"external_id":       "user_default",
		"organization_uuid": organizationUUID,
		"email":             "admin@example.local",
		"name":              "Local Admin",
	}); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, seedWorkspaceMemberQuery, map[string]any{
		"external_id":           "wmem_default",
		"organization_uuid":     organizationUUID,
		"workspace_uuid":        workspaceUUID,
		"workspace_external_id": "workspace_default",
		"user_uuid":             userUUID,
		"user_external_id":      "user_default",
	}); err != nil {
		return err
	}

	for _, key := range seedAPIKeys {
		if strings.TrimSpace(key.ExternalID) == "" || key.Key == "" {
			return errors.New("seed api keys must include external_id and key")
		}
		if _, err := namedExecContext(ctx, tx, seedAPIKeyQuery, map[string]any{
			"external_id":          key.ExternalID,
			"workspace_uuid":       workspaceUUID,
			"key_hash":             auth.HashAPIKey(key.Key),
			"created_by_user_uuid": userUUID,
			"name":                 key.ExternalID,
			"partial_key_hint":     partialAPIKeyHint(key.Key),
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) GetAPIKey(ctx context.Context, keyHash string) (APIKey, error) {
	var row apiKeyRow
	err := namedGetContext(ctx, d.sql, &row, getAPIKeyQuery, map[string]any{"key_hash": keyHash})
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	if err != nil {
		return APIKey{}, err
	}
	return row.apiKey(), nil
}

func (r apiKeyRow) apiKey() APIKey {
	return APIKey{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceUUID:       r.WorkspaceUUID,
		WorkspaceExternalID: r.WorkspaceExternalID,
	}
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

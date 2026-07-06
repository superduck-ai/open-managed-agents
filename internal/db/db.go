package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/user"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound             = platform.ErrNotFound
	ErrInvalidState         = errors.New("invalid state")
	ErrPreconditionFailed   = errors.New("precondition failed")
	ErrDuplicate            = errors.New("duplicate")
	ErrVersionConflict      = errors.New("version conflict")
	ErrWorkerEpochMismatch  = errors.New("worker epoch mismatch")
	ErrWorkerNotRegistered  = errors.New("worker not registered")
	ErrWorkerLeaseExpired   = errors.New("worker lease expired")
	ErrStorageLimitExceeded = errors.New("storage limit exceeded")
	ErrLimitExceeded        = errors.New("limit exceeded")
)

type DB struct {
	Pool *pgxpool.Pool
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

type FileRecord struct {
	ID                int64
	UUID              string
	ExternalID        string
	WorkspaceID       int64
	Filename          string
	MimeType          string
	SizeBytes         int64
	SHA256            string
	S3Bucket          string
	S3Key             string
	Downloadable      bool
	ScopeType         *string
	ScopeID           *string
	CreatedByAPIKeyID int64
	CreatedAt         time.Time
}

type ListFilesPageParams struct {
	WorkspaceID int64
	ScopeID     string
	AfterID     string
	BeforeID    string
	Limit       int
}

type ObjectCleanupJob struct {
	ID             int64
	ExternalID     string
	WorkspaceID    int64
	Bucket         string
	Key            string
	FileExternalID string
	Attempts       int
}

func Open(ctx context.Context, cfg config.Config) (*DB, error) {
	pool, err := openPool(ctx, cfg.DatabaseURL)
	if err == nil {
		return &DB{Pool: pool}, nil
	}

	if bootstrapErr := EnsureDatabase(ctx, cfg.DatabaseURL, cfg.PostgresAdminURL); bootstrapErr != nil {
		return nil, fmt.Errorf("connect database: %w; bootstrap database: %v", err, bootstrapErr)
	}

	pool, err = openPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect database after bootstrap: %w", err)
	}
	return &DB{Pool: pool}, nil
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
	if d != nil && d.Pool != nil {
		d.Pool.Close()
	}
}

func EnsureDatabase(ctx context.Context, databaseURL, adminURL string) error {
	candidates := []string{adminURL}
	for _, maintenanceDB := range []string{"postgres", "template1"} {
		if candidate, err := maintenanceURL(databaseURL, maintenanceDB); err == nil && !slices.Contains(candidates, candidate) {
			candidates = append(candidates, candidate)
		}
	}
	for _, candidate := range currentUserAdminURLs(databaseURL) {
		if !slices.Contains(candidates, candidate) {
			candidates = append(candidates, candidate)
		}
	}

	var errs []string
	for _, candidate := range candidates {
		if err := ensureDatabaseWithAdmin(ctx, databaseURL, candidate); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", redactPassword(candidate), err))
			continue
		}
		return nil
	}
	return fmt.Errorf("all admin connection attempts failed: %s", strings.Join(errs, "; "))
}

func ensureDatabaseWithAdmin(ctx context.Context, databaseURL, adminURL string) error {
	target, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	dbName := strings.TrimPrefix(target.Path, "/")
	if dbName == "" {
		return errors.New("DATABASE_URL must include a database name")
	}
	role := target.User.Username()
	password, _ := target.User.Password()
	if role == "" {
		return errors.New("DATABASE_URL must include a database user")
	}

	adminPool, err := openPool(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect POSTGRES_ADMIN_URL: %w", err)
	}
	defer adminPool.Close()

	var roleExists bool
	if err := adminPool.QueryRow(ctx, "select exists(select 1 from pg_roles where rolname=$1)", role).Scan(&roleExists); err != nil {
		return fmt.Errorf("check role: %w", err)
	}
	if !roleExists {
		if _, err := adminPool.Exec(ctx, fmt.Sprintf("create role %s login password %s", quoteIdent(role), quoteLiteral(password))); err != nil {
			return fmt.Errorf("create role %s: %w", role, err)
		}
	} else if password != "" {
		if _, err := adminPool.Exec(ctx, fmt.Sprintf("alter role %s with password %s", quoteIdent(role), quoteLiteral(password))); err != nil {
			return fmt.Errorf("alter role %s password: %w", role, err)
		}
	}

	var dbExists bool
	if err := adminPool.QueryRow(ctx, "select exists(select 1 from pg_database where datname=$1)", dbName).Scan(&dbExists); err != nil {
		return fmt.Errorf("check database: %w", err)
	}
	if !dbExists {
		if _, err := adminPool.Exec(ctx, fmt.Sprintf("create database %s owner %s", quoteIdent(dbName), quoteIdent(role))); err != nil {
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

func currentUserAdminURLs(databaseURL string) []string {
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

func (d *DB) WorkspaceStorageBytes(ctx context.Context, workspaceID int64) (int64, error) {
	var total int64
	err := d.Pool.QueryRow(ctx, `
		select coalesce(sum(size_bytes), 0)
		from files
		where workspace_id = $1 and deleted_at is null
	`, workspaceID).Scan(&total)
	return total, err
}

func (d *DB) CreateFile(ctx context.Context, f FileRecord) error {
	_, err := d.Pool.Exec(ctx, `
		insert into files (
			uuid, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, f.UUID, f.ExternalID, f.WorkspaceID, f.Filename, f.MimeType, f.SizeBytes, f.SHA256,
		f.S3Bucket, f.S3Key, f.Downloadable, f.ScopeType, f.ScopeID, f.CreatedByAPIKeyID, f.CreatedAt)
	return err
}

func (d *DB) CreateFileIfWithinLimit(ctx context.Context, f FileRecord, workspaceStorageLimitBytes int64) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, f.WorkspaceID); err != nil {
		return err
	}

	var total int64
	if err := tx.QueryRow(ctx, `
		select coalesce(sum(size_bytes), 0)
		from files
		where workspace_id = $1 and deleted_at is null
	`, f.WorkspaceID).Scan(&total); err != nil {
		return err
	}
	if workspaceStorageLimitBytes > 0 && total+f.SizeBytes > workspaceStorageLimitBytes {
		return ErrStorageLimitExceeded
	}

	if _, err := tx.Exec(ctx, `
		insert into files (
			uuid, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, f.UUID, f.ExternalID, f.WorkspaceID, f.Filename, f.MimeType, f.SizeBytes, f.SHA256,
		f.S3Bucket, f.S3Key, f.Downloadable, f.ScopeType, f.ScopeID, f.CreatedByAPIKeyID, f.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) GetFile(ctx context.Context, workspaceID int64, fileExternalID string) (FileRecord, error) {
	var f FileRecord
	err := d.Pool.QueryRow(ctx, `
		select id, uuid::text, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		from files
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, fileExternalID).Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
		&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileRecord{}, ErrNotFound
	}
	return f, err
}

func (d *DB) GetFileByUUID(ctx context.Context, workspaceID int64, fileUUID string) (FileRecord, error) {
	var f FileRecord
	err := d.Pool.QueryRow(ctx, `
		select id, uuid::text, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		from files
		where workspace_id = $1 and uuid::text = $2 and deleted_at is null
	`, workspaceID, fileUUID).Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
		&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileRecord{}, ErrNotFound
	}
	return f, err
}

func (d *DB) GetFileByUUIDInOrganization(ctx context.Context, organizationID int64, fileUUID string) (FileRecord, error) {
	var f FileRecord
	err := d.Pool.QueryRow(ctx, `
		select f.id, f.uuid::text, f.external_id, f.workspace_id, f.filename, f.mime_type, f.size_bytes, f.sha256,
			f.s3_bucket, f.s3_key, f.downloadable, f.scope_type, f.scope_id, f.created_by_api_key_id, f.created_at
		from files f
		join workspaces w on w.id = f.workspace_id
		where w.organization_id = $1 and f.uuid::text = $2 and f.deleted_at is null
	`, organizationID, fileUUID).Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
		&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileRecord{}, ErrNotFound
	}
	return f, err
}

func (d *DB) ListFiles(ctx context.Context, workspaceID int64, scopeID string) ([]FileRecord, error) {
	query := `
		select id, uuid::text, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		from files
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{workspaceID}
	if scopeID != "" {
		query += " and scope_id = $2"
		args = append(args, scopeID)
	}
	query += " order by created_at desc, id desc"

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
			&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (d *DB) ListFilesPage(ctx context.Context, params ListFilesPageParams) ([]FileRecord, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.AfterID != "" {
		params.BeforeID = ""
	}

	var cursorID int64
	var cursorCreatedAt time.Time
	if params.AfterID != "" || params.BeforeID != "" {
		cursorExternalID := params.AfterID
		if cursorExternalID == "" {
			cursorExternalID = params.BeforeID
		}
		query := `
			select id, created_at
			from files
			where workspace_id = $1 and external_id = $2 and deleted_at is null
		`
		args := []any{params.WorkspaceID, cursorExternalID}
		if params.ScopeID != "" {
			query += " and scope_id = $3"
			args = append(args, params.ScopeID)
		}
		err := d.Pool.QueryRow(ctx, query, args...).Scan(&cursorID, &cursorCreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
	}

	query := `
		select id, uuid::text, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		from files
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if params.ScopeID != "" {
		query += fmt.Sprintf(" and scope_id = $%d", nextArg)
		args = append(args, params.ScopeID)
		nextArg++
	}
	if params.AfterID != "" {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, cursorCreatedAt, cursorID)
		nextArg += 2
	} else if params.BeforeID != "" {
		query += fmt.Sprintf(" and (created_at > $%d or (created_at = $%d and id > $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, cursorCreatedAt, cursorID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	files, err := scanFileRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(files) > params.Limit
	if hasMore {
		files = files[:params.Limit]
	}
	return files, hasMore, nil
}

func (d *DB) SoftDeleteFile(ctx context.Context, workspaceID int64, fileExternalID string) error {
	tag, err := d.Pool.Exec(ctx, `
		update files
		set deleted_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, fileExternalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) EnqueueObjectCleanupJob(ctx context.Context, workspaceID int64, bucket, key, fileExternalID string) error {
	return d.EnqueueObjectCleanupResourceJob(ctx, workspaceID, bucket, key, "file", fileExternalID)
}

func (d *DB) EnqueueObjectCleanupResourceJob(ctx context.Context, workspaceID int64, bucket, key, resourceType, resourceID string) error {
	_, err := d.Pool.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values (
			concat('job_', replace(gen_random_uuid()::text, '-', '')),
			$1,
			'object_cleanup',
			'pending',
			jsonb_build_object(
				'bucket', $2::text,
				'key', $3::text,
				'file_id', case when $4::text = 'file' then $5::text else '' end,
				'resource_type', $4::text,
				'resource_id', $5::text
			)
		)
	`, workspaceID, bucket, key, resourceType, resourceID)
	return err
}

func (d *DB) LeaseObjectCleanupJobs(ctx context.Context, workerID string, limit int) ([]ObjectCleanupJob, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := d.Pool.Query(ctx, `
		with next_jobs as (
			select id
			from jobs
			where type = 'object_cleanup'
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at
			limit $1
			for update skip locked
		)
		update jobs j
		set status = 'running',
			locked_by = $2,
			locked_until = now() + interval '1 minute',
			updated_at = now()
		from next_jobs
		where j.id = next_jobs.id
		returning j.id, j.external_id, j.workspace_id,
			coalesce(j.payload->>'bucket', ''),
			coalesce(j.payload->>'key', ''),
			coalesce(j.payload->>'file_id', ''),
			j.attempts
	`, limit, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []ObjectCleanupJob
	for rows.Next() {
		var job ObjectCleanupJob
		if err := rows.Scan(&job.ID, &job.ExternalID, &job.WorkspaceID, &job.Bucket, &job.Key, &job.FileExternalID, &job.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *DB) CompleteObjectCleanupJob(ctx context.Context, jobID int64) error {
	_, err := d.Pool.Exec(ctx, `
		update jobs
		set status = 'completed',
			locked_by = null,
			locked_until = null,
			updated_at = now()
		where id = $1 and type = 'object_cleanup'
	`, jobID)
	return err
}

func (d *DB) FailObjectCleanupJob(ctx context.Context, jobID int64, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	_, err := d.Pool.Exec(ctx, `
		update jobs
		set status = $2,
			locked_by = null,
			locked_until = null,
			run_after = $3,
			updated_at = now(),
			attempts = $5,
			payload = payload || jsonb_build_object('last_error', $4::text)
		where id = $1 and type = 'object_cleanup'
	`, jobID, status, runAfter, reason, nextAttempts)
	return err
}

type fileRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanFileRows(rows fileRows) ([]FileRecord, error) {
	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
			&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

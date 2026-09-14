package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/user"
	"slices"
	"strings"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/yourbatis"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrNotFound                 = platform.ErrNotFound
	ErrInvalidState             = errors.New("invalid state")
	ErrPreconditionFailed       = errors.New("precondition failed")
	ErrDuplicate                = errors.New("duplicate")
	ErrVersionConflict          = errors.New("version conflict")
	ErrIncompleteSecretEnvelope = errors.New("incomplete vault credential secret envelope")
	ErrWorkerEpochMismatch      = errors.New("worker epoch mismatch")
	ErrWorkerNotRegistered      = errors.New("worker not registered")
	ErrWorkerLeaseExpired       = errors.New("worker lease expired")
	ErrStorageLimitExceeded     = errors.New("storage limit exceeded")
	ErrStorageUsageUnderflow    = errors.New("storage usage underflow")
	ErrLimitExceeded            = errors.New("limit exceeded")
	ErrMemoryStoreLimit         = errors.New("memory store limit exceeded")
	ErrFileInUse                = errors.New("file is in use")
	ErrFileReferenceNotFound    = errors.New("file reference not found")
	ErrStaleSchedule            = errors.New("stale deployment schedule")
	ErrWorkspaceArchived        = errors.New("workspace archived")
)

type DB struct {
	pool     *pgxpool.Pool
	mapperDB *yourbatis.DB
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

const (
	maintenanceRoleExistsQuery     = `select exists(select 1 from pg_roles where rolname = $1)`
	maintenanceDatabaseExistsQuery = `
		select exists(select 1 from pg_database where datname = $1)
	`
	idColumnDataTypeQuery = `
		select coalesce((
			select data_type
			from information_schema.columns
			where table_schema = current_schema()
				and table_name = $1
				and column_name = 'id'
		), '')
	`
	legacyTableExistsQuery = `select to_regclass($1) is not null`
)

func Open(ctx context.Context, cfg config.Config, logger *slog.Logger) (*DB, error) {
	pool, err := openPool(ctx, cfg.Database.URL)
	if err == nil {
		return newDB(pool, logger), nil
	}

	if bootstrapErr := EnsureDatabase(ctx, cfg.Database.URL); bootstrapErr != nil {
		return nil, fmt.Errorf("connect database: %w; bootstrap database: %v", err, bootstrapErr)
	}

	pool, err = openPool(ctx, cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("connect database after bootstrap: %w", err)
	}
	return newDB(pool, logger), nil
}

func newDB(pool *pgxpool.Pool, logger *slog.Logger) *DB {
	sqlDB := newStandardDB(pool)
	mapperDB := yourbatis.NewDB(
		sqlDB,
		yourbatis.DialectPostgres,
		yourbatis.WithDatabaseID("postgres"),
		yourbatis.WithLogger(yourbatis.SlogLogger{
			Logger: logging.LoggerOrDefault(logger),
		}),
	)
	return &DB{
		pool:     pool,
		mapperDB: mapperDB,
	}
}

func newStandardDB(pool *pgxpool.Pool) *sql.DB {
	// OpenDBFromPool 会把 database/sql 的 MaxIdleConns 固定为 0，避免包装层长期占住
	// pgxpool 连接；最大连接数与连接寿命继续由上面的唯一 pgxpool 约束。
	return stdlib.OpenDBFromPool(pool)
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
	if d.mapperDB != nil {
		_ = d.mapperDB.Close()
	}
	if d.pool != nil {
		d.pool.Close()
	}
}

// SQLDB exposes the Yourbatis-owned database/sql wrapper to integrations.
// Callers must not close it or use it for application queries.
func (d *DB) SQLDB() *sql.DB {
	return d.mapperDB.SQLDB()
}

// ListenerPool exposes the existing pool for integration-owned LISTEN connections.
// Callers must not close it or use it for application queries. Stop integrations
// before DB.Close so their dedicated listener connections are also closed.
func (d *DB) ListenerPool() *pgxpool.Pool {
	return d.pool
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
	maintenanceSQL := newStandardDB(maintenancePool)
	defer maintenanceSQL.Close()

	var roleExists bool
	if err := maintenanceSQL.QueryRowContext(ctx, maintenanceRoleExistsQuery, role).Scan(&roleExists); err != nil {
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
	if err := maintenanceSQL.QueryRowContext(ctx, maintenanceDatabaseExistsQuery, dbName).Scan(&dbExists); err != nil {
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

func idColumnDataType(ctx context.Context, database *sql.DB, table string) (string, error) {
	var dataType string
	err := database.QueryRowContext(ctx, idColumnDataTypeQuery, table).Scan(&dataType)
	return dataType, err
}

func migrateLegacyTextIDSchema(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables := []string{"jobs", "files", "api_keys", "workspaces", "organizations"}
	for _, table := range tables {
		legacy := table + "_legacy_text_ids"
		var exists bool
		if err := tx.QueryRowContext(ctx, legacyTableExistsQuery, legacy).Scan(&exists); err != nil {
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
	standardDB := newStandardDB(d.pool)
	defer standardDB.Close()

	dataType, err := idColumnDataType(ctx, standardDB, "organizations")
	if err != nil {
		return err
	}
	if dataType == "text" {
		if err := migrateLegacyTextIDSchema(ctx, standardDB); err != nil {
			return err
		}
	}
	if err := runGooseMigrations(ctx, standardDB); err != nil {
		return err
	}
	return dropForeignKeyConstraints(ctx, standardDB)
}

func (d *DB) DropForeignKeyConstraints(ctx context.Context) error {
	standardDB := newStandardDB(d.pool)
	defer standardDB.Close()
	return dropForeignKeyConstraints(ctx, standardDB)
}

func dropForeignKeyConstraints(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, `
		select cls.relname as table_name, con.conname as constraint_name
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where con.contype = 'f'
			and ns.oid = CAST(current_schema() AS regnamespace)
		order by cls.relname, con.conname
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var constraints []foreignKeyRow
	for rows.Next() {
		var constraint foreignKeyRow
		if err := rows.Scan(&constraint.Table, &constraint.Name); err != nil {
			return err
		}
		constraints = append(constraints, constraint)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, fk := range constraints {
		if _, err := database.ExecContext(ctx, fmt.Sprintf("alter table %s drop constraint %s", quoteIdent(fk.Table), quoteIdent(fk.Name))); err != nil {
			return fmt.Errorf("drop foreign key %s on %s: %w", fk.Name, fk.Table, err)
		}
	}
	return nil
}

func (d *DB) Seed(ctx context.Context, seedAPIKeys []config.SeedAPIKey) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		organizationMapper := NewAdminOrganizationMapper(executor)
		workspaceMapper := NewAdminWorkspaceMapper(executor)
		userMapper := NewAdminUserMapper(executor)
		memberMapper := NewAdminWorkspaceMemberMapper(executor)
		apiKeyMapper := NewAdminAPIKeyMapper(executor)

		if _, err := organizationMapper.LockSeed(ctx); err != nil {
			return err
		}
		organizationUUID, err := organizationMapper.SeedDefault(ctx, "workspace_default", "default")
		if err != nil {
			return err
		}
		workspaceUUID, err := workspaceMapper.SeedDefault(ctx, "workspace_default", organizationUUID, "default")
		if err != nil {
			return err
		}
		userUUID, err := userMapper.SeedDefault(ctx, "user_default", organizationUUID, "admin@example.local", "Local Admin")
		if err != nil {
			return err
		}
		if err := memberMapper.SeedDefault(ctx, seedAdminWorkspaceMemberParams{
			ExternalID:          "wmem_default",
			OrganizationUUID:    organizationUUID,
			WorkspaceUUID:       workspaceUUID,
			WorkspaceExternalID: "workspace_default",
			UserUUID:            userUUID,
			UserExternalID:      "user_default",
		}); err != nil {
			return err
		}

		for _, key := range seedAPIKeys {
			if strings.TrimSpace(key.ExternalID) == "" || key.Key == "" {
				return errors.New("seed api keys must include external_id and key")
			}
			if err := apiKeyMapper.SeedDefault(ctx, seedAdminAPIKeyParams{
				ExternalID:        key.ExternalID,
				WorkspaceUUID:     workspaceUUID,
				KeyHash:           auth.HashAPIKey(key.Key),
				CreatedByUserUUID: userUUID,
				Name:              key.ExternalID,
				PartialKeyHint:    partialAPIKeyHint(key.Key),
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) GetAPIKey(ctx context.Context, keyHash string) (APIKey, error) {
	mapper := NewAdminAPIKeyMapper(d.mapperDB)
	row, err := mapper.FindActiveByKeyHash(ctx, keyHash)
	if err != nil {
		return APIKey{}, mapNoRows(err)
	}
	return row.apiKey()
}

func (r apiKeyAuthRow) apiKey() (APIKey, error) {
	keyUUID, err := parseDBUUID("api_key_uuid", r.UUID)
	if err != nil {
		return APIKey{}, err
	}
	organizationUUID, err := parseDBUUID("organization_uuid", r.OrganizationUUID)
	if err != nil {
		return APIKey{}, err
	}
	workspaceUUID, err := parseDBUUID("workspace_uuid", r.WorkspaceUUID)
	if err != nil {
		return APIKey{}, err
	}
	return APIKey{
		UUID:                keyUUID,
		ExternalID:          r.ExternalID,
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		WorkspaceExternalID: r.WorkspaceExternalID,
	}, nil
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

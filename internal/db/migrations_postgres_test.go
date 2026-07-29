package db

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const migrationBackfillFixtureSQL = `
	insert into organizations (uuid, external_id, name)
	values ('10000000-0000-0000-0000-000000000001', 'org_migration_184', 'migration 184');

	insert into workspaces (uuid, external_id, organization_id, name)
	select '20000000-0000-0000-0000-000000000001', 'workspace_migration_184', id, 'migration 184'
	from organizations where external_id = 'org_migration_184';

	insert into api_keys (uuid, external_id, workspace_id, key_hash)
	select '30000000-0000-0000-0000-000000000001', 'api_key_migration_184', id, 'migration-184-hash'
	from workspaces where external_id = 'workspace_migration_184';

	insert into sessions (
		uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
		environment_id, environment_external_id, agent_id, agent_external_id,
		agent_version, agent_snapshot
	)
	select
		'40000000-0000-0000-0000-000000000001', 'sesn_migration_184',
		o.id, w.id, ak.id, 1, 'env_migration_184', 1, 'agent_migration_184', 1, '{}'
	from organizations o
	join workspaces w on w.organization_id = o.id
	join api_keys ak on ak.workspace_id = w.id
	where o.external_id = 'org_migration_184';

	insert into session_resources (
		uuid, external_id, organization_id, workspace_id, session_id,
		session_external_id, resource_type, payload
	)
	select
		'50000000-0000-0000-0000-000000000001', 'sesrsc_input_migration_184',
		o.id, w.id, s.id, s.external_id, 'file', '{"file_id":"file_source_migration_184"}'
	from sessions s
	join organizations o on o.id = s.organization_id
	join workspaces w on w.id = s.workspace_id
	where s.external_id = 'sesn_migration_184';

	insert into files (
		uuid, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
		s3_bucket, s3_key, downloadable, created_by_api_key_id
	)
	select
		'60000000-0000-0000-0000-000000000001', 'file_source_migration_184',
		w.id, 'input.txt', 'text/plain', 11, repeat('a', 64),
		'migration-184', 'source/input.txt', true, ak.id
	from workspaces w
	join api_keys ak on ak.workspace_id = w.id
	where w.external_id = 'workspace_migration_184';

	insert into filestore_filesystems (
		uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
		created_by_api_key_uuid
	)
	values (
		'70000000-0000-0000-0000-000000000001', 'claude_chat_migration_184',
		'10000000-0000-0000-0000-000000000001',
		'20000000-0000-0000-0000-000000000001',
		'40000000-0000-0000-0000-000000000001',
		'30000000-0000-0000-0000-000000000001'
	);

	insert into filestore_entries (
		uuid, external_id, organization_uuid, workspace_uuid, filesystem_uuid,
		kind, path, parent_path, size_bytes, media_type, detected_mime_type,
		metadata, authorization_metadata, tags, downloadable, md5, sha256,
		s3_bucket, s3_key, managed_by, managed_resource_uuid, source_file_uuid,
		created_by_api_key_uuid, created_by_session_uuid
	)
	values (
		'80000000-0000-0000-0000-000000000001', 'fse_input_migration_184',
		'10000000-0000-0000-0000-000000000001',
		'20000000-0000-0000-0000-000000000001',
		'70000000-0000-0000-0000-000000000001',
		'file', '/uploads/input.txt', '/uploads', 11, 'text/plain', 'text/plain',
		'{"origin":"input"}', '{"policy":"download"}', array['input'], true,
		null, repeat('a', 64), 'migration-184', 'source/input.txt',
		'session_file_resource', '50000000-0000-0000-0000-000000000001',
		'60000000-0000-0000-0000-000000000001',
		'30000000-0000-0000-0000-000000000001',
		'40000000-0000-0000-0000-000000000001'
	), (
		'80000000-0000-0000-0000-000000000002', 'fse_output_migration_184',
		'10000000-0000-0000-0000-000000000001',
		'20000000-0000-0000-0000-000000000001',
		'70000000-0000-0000-0000-000000000001',
		'file', '/outputs/output.txt', '/outputs', 12, 'text/plain', 'text/plain',
		'{"origin":"output"}', '{"policy":"session"}', array['output'], true,
		'output-md5', repeat('b', 64), 'migration-184', 'owned/output.txt',
		null, null, null,
		'30000000-0000-0000-0000-000000000001',
		'40000000-0000-0000-0000-000000000001'
	), (
		'80000000-0000-0000-0000-000000000003', 'fse_expired_output_migration_184',
		'10000000-0000-0000-0000-000000000001',
		'20000000-0000-0000-0000-000000000001',
		'70000000-0000-0000-0000-000000000001',
		'file', '/outputs/expired.txt', '/outputs', 13, 'text/plain', 'text/plain',
		'{"origin":"expired-output"}', '{"policy":"session"}', array['expired'], true,
		'expired-md5', repeat('d', 64), 'migration-184', 'owned/expired.txt',
		null, null, null,
		'30000000-0000-0000-0000-000000000001',
		'40000000-0000-0000-0000-000000000001'
	);

	update filestore_entries
	set expires_at = to_timestamp(0)
	where external_id = 'fse_expired_output_migration_184';

	insert into files (
		uuid, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
		s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id
	)
	select
		'80000000-0000-0000-0000-000000000002', 'file_output_migration_184',
		w.id, 'stale-output.txt', 'application/octet-stream', 1, repeat('c', 64),
		'migration-184', 'stale/output.txt', false, 'session', 'sesn_migration_184', ak.id
	from workspaces w
	join api_keys ak on ak.workspace_id = w.id
	where w.external_id = 'workspace_migration_184';
`

func TestUnifySessionResourcesAndFilesMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(pool.Close)
	standardDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = standardDB.Close() })

	provider := newMigrationTestProvider(t, standardDB)
	if _, err := provider.UpTo(ctx, 33); err != nil {
		t.Fatalf("migrate fixture database to 33: %v", err)
	}
	if _, err := standardDB.ExecContext(ctx, migrationBackfillFixtureSQL); err != nil {
		t.Fatalf("seed migration fixture: %v", err)
	}
	if _, err := provider.UpTo(ctx, 34); err != nil {
		t.Fatalf("create legacy Input projection: %v", err)
	}
	if _, err := standardDB.ExecContext(ctx, `
		update filestore_entries
		set managed_resource_uuid = '50000000-0000-0000-0000-000000000099'
		where external_id = 'fse_input_migration_184'
	`); err != nil {
		t.Fatalf("break legacy Input reference: %v", err)
	}
	if _, err := provider.UpTo(ctx, 36); err == nil {
		t.Fatal("migration accepted an unresolved legacy Input reference")
	}
	var oldTableExists bool
	if err := standardDB.QueryRowContext(ctx, `select to_regclass('filestore_entries') is not null`).Scan(&oldTableExists); err != nil {
		t.Fatalf("check old table after rejected migration: %v", err)
	}
	if !oldTableExists {
		t.Fatal("rejected migration dropped filestore_entries")
	}
	if _, err := standardDB.ExecContext(ctx, `
		update filestore_entries
		set managed_resource_uuid = '50000000-0000-0000-0000-000000000001'
		where external_id = 'fse_input_migration_184'
	`); err != nil {
		t.Fatalf("restore legacy Input reference: %v", err)
	}

	if _, err := provider.UpTo(ctx, 36); err != nil {
		t.Fatalf("migrate fixture database to 36: %v", err)
	}

	assertUnifiedMigrationState(t, ctx, standardDB)
}

func newMigrationTestProvider(t *testing.T, standardDB *sql.DB) *goose.Provider {
	t.Helper()
	migrationFS, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("open embedded migrations: %v", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		standardDB,
		migrationFS,
		goose.WithDisableGlobalRegistry(true),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		t.Fatalf("create migration provider: %v", err)
	}
	return provider
}

func assertUnifiedMigrationState(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()

	var oldTableExists bool
	if err := database.QueryRowContext(ctx, `select to_regclass('filestore_entries') is not null`).Scan(&oldTableExists); err != nil {
		t.Fatalf("check old table: %v", err)
	}
	if oldTableExists {
		t.Fatal("filestore_entries still exists after migration")
	}

	var path, sourceUUID string
	if err := database.QueryRowContext(ctx, `
		select path, cast(file_uuid as text)
		from session_resources
		where uuid = '50000000-0000-0000-0000-000000000001'
	`).Scan(&path, &sourceUUID); err != nil {
		t.Fatalf("load migrated Input Resource: %v", err)
	}
	if path != "/uploads/input.txt" || sourceUUID != "60000000-0000-0000-0000-000000000001" {
		t.Fatalf("migrated Input Resource = path %q, file %q", path, sourceUUID)
	}

	var aliasColumnExists bool
	if err := database.QueryRowContext(ctx, `
		select exists (
			select 1 from information_schema.columns
			where table_schema = current_schema()
				and table_name = 'session_resources'
				and column_name = 'session_file_external_id'
		)
	`).Scan(&aliasColumnExists); err != nil {
		t.Fatalf("check removed Session File Alias column: %v", err)
	}
	if aliasColumnExists {
		t.Fatal("session_file_external_id still exists after migration")
	}

	var fileCount int
	if err := database.QueryRowContext(ctx, `select count(*) from files`).Scan(&fileCount); err != nil {
		t.Fatalf("count migrated Files: %v", err)
	}
	if fileCount != 3 {
		t.Fatalf("files count = %d, want source plus active and expired outputs", fileCount)
	}

	var outputExternalID, outputFilename, outputKey string
	var outputSize int64
	if err := database.QueryRowContext(ctx, `
		select external_id, filename, size_bytes, s3_key
		from files where uuid = '80000000-0000-0000-0000-000000000002'
	`).Scan(&outputExternalID, &outputFilename, &outputSize, &outputKey); err != nil {
		t.Fatalf("load migrated Output File: %v", err)
	}
	if outputExternalID != "file_output_migration_184" || outputFilename != "output.txt" || outputSize != 12 || outputKey != "owned/output.txt" {
		t.Fatalf("migrated Output File = id %q, filename %q, size %d, key %q", outputExternalID, outputFilename, outputSize, outputKey)
	}

	var resourceUUID, outputFileUUID string
	if err := database.QueryRowContext(ctx, `
		select cast(uuid as text), cast(file_uuid as text)
		from session_resources where path = '/outputs/output.txt'
	`).Scan(&resourceUUID, &outputFileUUID); err != nil {
		t.Fatalf("load migrated Output Resource: %v", err)
	}
	if outputFileUUID != "80000000-0000-0000-0000-000000000002" || resourceUUID == outputFileUUID {
		t.Fatalf("migrated Output Resource = resource %q, file %q", resourceUUID, outputFileUUID)
	}

	var expiredPath, expiredFileUUID string
	var expiresAtValid bool
	if err := database.QueryRowContext(ctx, `
		select path, cast(file_uuid as text), expires_at is not null
		from session_resources where path = '/outputs/expired.txt'
	`).Scan(&expiredPath, &expiredFileUUID, &expiresAtValid); err != nil {
		t.Fatalf("load migrated expired Output Resource: %v", err)
	}
	if expiredPath != "/outputs/expired.txt" ||
		expiredFileUUID != "80000000-0000-0000-0000-000000000003" ||
		!expiresAtValid {
		t.Fatalf(
			"migrated expired Output = path %q, file %q, expires_at present %t",
			expiredPath,
			expiredFileUUID,
			expiresAtValid,
		)
	}
}

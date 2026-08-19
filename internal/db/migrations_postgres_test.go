package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/superduck-ai/yourbatis"
)

func TestSessionResourceFileOwnershipMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}

	ctx := context.Background()
	standardDB := newIsolatedMigrationTestDatabase(t, ctx, databaseURL)
	provider := newMigrationTestProvider(t, standardDB)
	if _, err := provider.UpTo(ctx, 48); err != nil {
		t.Fatalf("migrate ownership fixture database to 48: %v", err)
	}
	if _, err := standardDB.ExecContext(ctx, sessionFileOwnershipMigrationFixtureSQL); err != nil {
		t.Fatalf("seed ownership migration fixture: %v", err)
	}

	if _, err := standardDB.ExecContext(ctx, `
		update files
		set workspace_uuid = '20000000-0000-0000-0000-000000000002'
		where external_id = 'file_source_ownership'
	`); err != nil {
		t.Fatalf("break referenced File workspace: %v", err)
	}
	if _, err := provider.UpTo(ctx, 52); err == nil {
		t.Fatal("ownership migration accepted a cross-workspace File reference")
	}
	assertMigrationColumnExists(t, ctx, standardDB, "file_ownership", false)
	if _, err := standardDB.ExecContext(ctx, `
		update files
		set workspace_uuid = '20000000-0000-0000-0000-000000000001'
		where external_id = 'file_source_ownership'
	`); err != nil {
		t.Fatalf("restore referenced File workspace: %v", err)
	}

	if _, err := standardDB.ExecContext(ctx, `
		update session_resources
		set payload = jsonb_set(payload, '{mount_path}', '"/wrong.txt"')
		where external_id = 'sesrsc_input_ownership'
	`); err != nil {
		t.Fatalf("break referenced Resource backing path: %v", err)
	}
	if _, err := provider.UpTo(ctx, 52); err == nil {
		t.Fatal("ownership migration accepted an inconsistent referenced Resource payload")
	}
	assertMigrationColumnExists(t, ctx, standardDB, "file_ownership", false)
	if _, err := standardDB.ExecContext(ctx, `
		update session_resources
		set payload = jsonb_set(payload, '{mount_path}', '"/uploads/input.txt"')
		where external_id = 'sesrsc_input_ownership'
	`); err != nil {
		t.Fatalf("restore referenced Resource backing path: %v", err)
	}

	if _, err := standardDB.ExecContext(ctx, `
		update files
		set scope_id = 'sesn_other_ownership'
		where external_id = 'file_owned_ownership'
	`); err != nil {
		t.Fatalf("break owned File Session scope: %v", err)
	}
	if _, err := provider.UpTo(ctx, 52); err == nil {
		t.Fatal("ownership migration accepted an owned File scoped to another Session")
	}
	assertMigrationColumnExists(t, ctx, standardDB, "file_ownership", false)
	if _, err := standardDB.ExecContext(ctx, `
		update files
		set scope_id = 'sesn_file_ownership'
		where external_id = 'file_owned_ownership'
	`); err != nil {
		t.Fatalf("restore owned File Session scope: %v", err)
	}

	if _, err := standardDB.ExecContext(ctx, `
		insert into session_resources (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			session_external_id, resource_type, payload, path, parent_path, file_uuid
		)
		values (
			'50000000-0000-0000-0000-000000000004', 'sesrsc_mixed_reference_ownership',
			'10000000-0000-0000-0000-000000000001',
			'20000000-0000-0000-0000-000000000001',
			'40000000-0000-0000-0000-000000000001', 'sesn_file_ownership',
			'file', '{"id":"sesrsc_mixed_reference_ownership","type":"file","file_id":"file_owned_ownership","source":"/uploads","mount_path":"/mixed.txt"}',
			'/uploads/mixed.txt', '/uploads',
			'60000000-0000-0000-0000-000000000002'
		)
	`); err != nil {
		t.Fatalf("seed mixed referenced/owned File: %v", err)
	}
	if _, err := provider.UpTo(ctx, 52); err == nil {
		t.Fatal("ownership migration accepted a File shared by owned and referenced Resources")
	}
	assertMigrationColumnExists(t, ctx, standardDB, "file_ownership", false)
	if _, err := standardDB.ExecContext(ctx, `
		delete from session_resources where external_id = 'sesrsc_mixed_reference_ownership'
	`); err != nil {
		t.Fatalf("remove mixed ownership fixture: %v", err)
	}

	if _, err := provider.UpTo(ctx, 52); err != nil {
		t.Fatalf("migrate ownership fixture database to 52: %v", err)
	}
	assertMigrationColumnExists(t, ctx, standardDB, "file_ownership", true)
	rows, err := standardDB.QueryContext(ctx, `
		select path, coalesce(file_ownership, '')
		from session_resources
		where path in ('/uploads/input.txt', '/outputs/output.txt', '/skills/demo')
		order by path
	`)
	if err != nil {
		t.Fatalf("load migrated ownership values: %v", err)
	}
	defer rows.Close()
	got := make(map[string]string)
	for rows.Next() {
		var path, ownership string
		if err := rows.Scan(&path, &ownership); err != nil {
			t.Fatalf("scan migrated ownership: %v", err)
		}
		got[path] = ownership
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migrated ownership values: %v", err)
	}
	if got["/uploads/input.txt"] != "referenced" ||
		got["/outputs/output.txt"] != "owned" ||
		got["/skills/demo"] != "" {
		t.Fatalf("migrated ownership values = %#v", got)
	}
	if _, err := standardDB.ExecContext(ctx, `
		update session_resources set file_ownership = null
		where path = '/outputs/output.txt'
	`); err == nil {
		t.Fatal("ownership migration constraint accepted a File with NULL ownership")
	}
	if _, err := provider.Down(ctx); err == nil {
		t.Fatal("ownership migration allowed an unsafe down migration")
	}
	assertMigrationColumnExists(t, ctx, standardDB, "file_ownership", true)
}

func newIsolatedMigrationTestDatabase(t *testing.T, ctx context.Context, databaseURL string) *sql.DB {
	t.Helper()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open migration admin database: %v", err)
	}
	schema := "migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, "create schema "+quoteIdent(schema)); err != nil {
		adminPool.Close()
		t.Fatalf("create migration test schema: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		adminPool.Close()
		t.Fatalf("parse migration database URL: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "drop schema "+quoteIdent(schema)+" cascade")
		adminPool.Close()
		t.Fatalf("open isolated migration schema: %v", err)
	}
	standardDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() {
		_ = standardDB.Close()
		pool.Close()
		_, _ = adminPool.Exec(context.Background(), "drop schema "+quoteIdent(schema)+" cascade")
		adminPool.Close()
	})
	return standardDB
}

func assertMigrationColumnExists(t *testing.T, ctx context.Context, database *sql.DB, column string, want bool) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `
		select exists (
			select 1
			from information_schema.columns
			where table_schema = current_schema()
				and table_name = 'session_resources'
				and column_name = $1
		)
	`, column).Scan(&exists); err != nil {
		t.Fatalf("check migration column %s: %v", column, err)
	}
	if exists != want {
		t.Fatalf("migration column %s exists = %t, want %t", column, exists, want)
	}
}

const sessionFileOwnershipMigrationFixtureSQL = `
	insert into organizations (uuid, name)
	values ('10000000-0000-0000-0000-000000000001', 'ownership migration');

	insert into workspaces (uuid, external_id, organization_uuid, name)
	values
		('20000000-0000-0000-0000-000000000001', 'workspace_ownership_one',
		 '10000000-0000-0000-0000-000000000001', 'ownership one'),
		('20000000-0000-0000-0000-000000000002', 'workspace_ownership_two',
		 '10000000-0000-0000-0000-000000000001', 'ownership two');

	insert into api_keys (uuid, external_id, workspace_uuid, key_hash)
	values ('30000000-0000-0000-0000-000000000001', 'api_key_ownership',
		'20000000-0000-0000-0000-000000000001', 'ownership-hash');

	insert into sessions (
		uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
		environment_uuid, environment_external_id, agent_uuid, agent_external_id,
		agent_version, agent_snapshot
	)
	values (
		'40000000-0000-0000-0000-000000000001', 'sesn_file_ownership',
		'10000000-0000-0000-0000-000000000001',
		'20000000-0000-0000-0000-000000000001',
		'30000000-0000-0000-0000-000000000001',
		'41000000-0000-0000-0000-000000000001', 'env_file_ownership',
		'42000000-0000-0000-0000-000000000001', 'agent_file_ownership', 1, '{}'
	);

	insert into files (
		uuid, external_id, workspace_uuid, filename, mime_type, size_bytes, sha256,
		s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_uuid
	)
	values
		('60000000-0000-0000-0000-000000000001', 'file_source_ownership',
		 '20000000-0000-0000-0000-000000000001', 'input.txt', 'text/plain', 11,
		 repeat('a', 64), 'ownership', 'source/input.txt', true, null, null,
		 '30000000-0000-0000-0000-000000000001'),
		('60000000-0000-0000-0000-000000000002', 'file_owned_ownership',
		 '20000000-0000-0000-0000-000000000001', 'output.txt', 'text/plain', 12,
		 repeat('b', 64), 'ownership', 'owned/output.txt', true, 'session', 'sesn_file_ownership',
		 '30000000-0000-0000-0000-000000000001'),
		('60000000-0000-0000-0000-000000000003', 'file_skill_ownership',
		 '20000000-0000-0000-0000-000000000001', 'demo.zip', 'application/zip', 13,
		 repeat('c', 64), 'ownership', 'skills/demo.zip', false, null, null,
		 '30000000-0000-0000-0000-000000000001');

	insert into session_resources (
		uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
		session_external_id, resource_type, payload, path, parent_path, file_uuid
	)
	values
		('50000000-0000-0000-0000-000000000001', 'sesrsc_input_ownership',
		 '10000000-0000-0000-0000-000000000001',
		 '20000000-0000-0000-0000-000000000001',
		 '40000000-0000-0000-0000-000000000001', 'sesn_file_ownership',
		 'file', '{"id":"sesrsc_input_ownership","type":"file","file_id":"file_source_ownership","source":"/uploads","mount_path":"/uploads/input.txt"}', '/uploads/input.txt', '/uploads',
		 '60000000-0000-0000-0000-000000000001'),
		('50000000-0000-0000-0000-000000000002', 'sesrsc_output_ownership',
		 '10000000-0000-0000-0000-000000000001',
		 '20000000-0000-0000-0000-000000000001',
		 '40000000-0000-0000-0000-000000000001', 'sesn_file_ownership',
		 'file', null, '/outputs/output.txt', '/outputs',
		 '60000000-0000-0000-0000-000000000002'),
		('50000000-0000-0000-0000-000000000003', 'sesrsc_skill_ownership',
		 '10000000-0000-0000-0000-000000000001',
		 '20000000-0000-0000-0000-000000000001',
		 '40000000-0000-0000-0000-000000000001', 'sesn_file_ownership',
		 'skill_archive', null, '/skills/demo', '/skills',
		 '60000000-0000-0000-0000-000000000003');
`

const migrationBackfillFixtureSQL = `
	insert into organizations (uuid, external_id, name)
	values ('10000000-0000-0000-0000-000000000001', 'org_migration_184', 'migration 184');

	insert into workspaces (uuid, external_id, organization_id, name)
	select '20000000-0000-0000-0000-000000000001', 'workspace_migration_184', id, 'migration 184'
	from organizations where external_id = 'org_migration_184';

	insert into api_keys (uuid, external_id, workspace_id, key_hash)
	select '30000000-0000-0000-0000-000000000001', 'api_key_migration_184', id, 'migration-184-hash'
	from workspaces where external_id = 'workspace_migration_184';

	insert into skills (
		uuid, external_id, workspace_id, created_by_api_key_id,
		display_title, latest_version, source
	)
	select
		'31000000-0000-0000-0000-000000000001', 'skill_migration_184',
		w.id, ak.id, 'migration skill', 'v1', 'custom'
	from workspaces w
	join api_keys ak on ak.workspace_id = w.id
	where w.external_id = 'workspace_migration_184';

	insert into skill_versions (
		uuid, external_id, workspace_id, skill_id, skill_external_id,
		version, name, directory, s3_bucket, s3_key, size_bytes, sha256,
		created_by_api_key_id
	)
	select
		'32000000-0000-0000-0000-000000000001', 'skver_migration_184',
		w.id, skill.id, skill.external_id, 'v1', 'Migration Skill', 'migration-skill',
		'migration-184', 'skills/migration-skill.zip', 128, repeat('e', 64), ak.id
	from skills skill
	join workspaces w on w.id = skill.workspace_id
	join api_keys ak on ak.workspace_id = w.id
	where skill.external_id = 'skill_migration_184';

	insert into agents (
		uuid, external_id, workspace_id, created_by_api_key_id, name, model
	)
	select
		'33000000-0000-0000-0000-000000000001', 'agent_migration_184',
		w.id, ak.id, 'Migration Agent', '{}'
	from workspaces w
	join api_keys ak on ak.workspace_id = w.id
	where w.external_id = 'workspace_migration_184';

	insert into environments (
		uuid, external_id, organization_id, workspace_id,
		created_by_api_key_id, name
	)
	select
		'34000000-0000-0000-0000-000000000001', 'env_migration_184',
		o.id, w.id, ak.id, 'Migration Environment'
	from organizations o
	join workspaces w on w.organization_id = o.id
	join api_keys ak on ak.workspace_id = w.id
	where w.external_id = 'workspace_migration_184';

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
	), (
		'80000000-0000-0000-0000-000000000004', 'fse_skill_migration_184',
		'10000000-0000-0000-0000-000000000001',
		'20000000-0000-0000-0000-000000000001',
		'70000000-0000-0000-0000-000000000001',
		'archive', '/skills/migration-skill', '/skills', 128,
		'application/zip', 'application/zip', '{"skill_source":"custom"}', '{}',
		array[]::text[], false, null, repeat('e', 64), 'migration-184',
		'skills/migration-skill.zip', 'skill_archive',
		'32000000-0000-0000-0000-000000000001', null,
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
	if _, err := standardDB.ExecContext(ctx, `
		update session_resources
		set skill_version_uuid = '32000000-0000-0000-0000-000000000099'
		where path = '/skills/migration-skill'
	`); err != nil {
		t.Fatalf("break legacy Skill Version reference: %v", err)
	}
	if _, err := provider.UpTo(ctx, 37); err == nil {
		t.Fatal("migration accepted an unresolved active Skill Version reference")
	}
	var skillVersionUUID string
	if err := standardDB.QueryRowContext(ctx, `
		select cast(skill_version_uuid as text)
		from session_resources
		where path = '/skills/migration-skill'
	`).Scan(&skillVersionUUID); err != nil {
		t.Fatalf("load Skill Version reference after rejected migration: %v", err)
	}
	if skillVersionUUID != "32000000-0000-0000-0000-000000000099" {
		t.Fatalf("rejected migration changed Skill Version reference to %q", skillVersionUUID)
	}
	if _, err := standardDB.ExecContext(ctx, `
		update session_resources
		set skill_version_uuid = '32000000-0000-0000-0000-000000000001'
		where path = '/skills/migration-skill'
	`); err != nil {
		t.Fatalf("restore legacy Skill Version reference: %v", err)
	}
	if _, err := provider.UpTo(ctx, 37); err != nil {
		t.Fatalf("migrate fixture database to 37: %v", err)
	}
	if _, err := standardDB.ExecContext(ctx, `
		update session_resources
		set organization_id = 0
		where path = '/skills/migration-skill'
	`); err != nil {
		t.Fatalf("break Session Resource tenant reference: %v", err)
	}
	if _, err := provider.UpTo(ctx, 38); err == nil {
		t.Fatal("migration accepted an invalid Session Resource tenant chain")
	}
	if _, err := standardDB.ExecContext(ctx, `
		update session_resources resource
		set organization_id = session.organization_id
		from sessions session
		where session.id = resource.session_id
			and resource.path = '/skills/migration-skill'
	`); err != nil {
		t.Fatalf("restore Session Resource tenant reference: %v", err)
	}
	if _, err := provider.UpTo(ctx, 38); err != nil {
		t.Fatalf("migrate Session Resource tenant references to UUID: %v", err)
	}

	assertUnifiedMigrationState(t, ctx, standardDB)
	if _, err := provider.Down(ctx); err != nil {
		t.Fatalf("roll back Session Resource tenant UUID migration: %v", err)
	}
	var restoredWorkspaceID, restoredSessionID int64
	if err := standardDB.QueryRowContext(ctx, `
		select workspace_id, session_id
		from session_resources
		where external_id = 'sesrsc_input_migration_184'
	`).Scan(&restoredWorkspaceID, &restoredSessionID); err != nil {
		t.Fatalf("load restored Session Resource internal IDs: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("reapply Session Resource tenant UUID migration: %v", err)
	}
	if _, err := provider.UpTo(ctx, 45); err != nil {
		t.Fatalf("migrate Session runtime tenant references to UUID: %v", err)
	}
	assertSessionResourceRuntimeWriteAfterUUIDMigration(t, ctx, standardDB)
}

func assertSessionResourceRuntimeWriteAfterUUIDMigration(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
) {
	t.Helper()
	var organizationUUID, workspaceUUID string
	if err := database.QueryRowContext(ctx, `
		select organization_uuid, workspace_uuid
		from sessions
		where external_id = 'sesn_migration_184'
	`).Scan(&organizationUUID, &workspaceUUID); err != nil {
		t.Fatalf("load migrated Session tenant IDs: %v", err)
	}
	createdAt := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	mapperDB := yourbatis.NewDB(database, yourbatis.DialectPostgres, yourbatis.WithDatabaseID("postgres"))
	created, err := createSessionResource(ctx, mapperDB, SessionResource{
		UUID:              "50000000-0000-0000-0000-000000000099",
		ExternalID:        "sesrsc_runtime_after_uuid_migration",
		OrganizationUUID:  organizationUUID,
		WorkspaceUUID:     workspaceUUID,
		SessionExternalID: "sesn_migration_184",
		ResourceType:      "github_repository",
		Payload:           json.RawMessage(`{"repository":"example/repository"}`),
		CreatedAt:         createdAt,
	})
	if err != nil {
		t.Fatalf("create Session Resource after UUID migration: %v", err)
	}
	if created.OrganizationUUID != organizationUUID || created.WorkspaceUUID != workspaceUUID {
		t.Fatalf(
			"created Session Resource tenant UUIDs = (%s, %s), want (%s, %s)",
			created.OrganizationUUID,
			created.WorkspaceUUID,
			organizationUUID,
			workspaceUUID,
		)
	}
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

	var skillVersionColumnExists bool
	if err := database.QueryRowContext(ctx, `
		select exists (
			select 1 from information_schema.columns
			where table_schema = current_schema()
				and table_name = 'session_resources'
				and column_name = 'skill_version_uuid'
		)
	`).Scan(&skillVersionColumnExists); err != nil {
		t.Fatalf("check removed Skill Version column: %v", err)
	}
	if skillVersionColumnExists {
		t.Fatal("skill_version_uuid still exists after migration")
	}

	var tenantUUIDs string
	if err := database.QueryRowContext(ctx, `
		select concat(
			cast(organization_uuid as text), '/',
			cast(workspace_uuid as text), '/',
			cast(session_uuid as text)
		)
		from session_resources
		where path = '/skills/migration-skill'
	`).Scan(&tenantUUIDs); err != nil {
		t.Fatalf("load migrated Session Resource tenant UUIDs: %v", err)
	}
	if tenantUUIDs != "10000000-0000-0000-0000-000000000001/20000000-0000-0000-0000-000000000001/40000000-0000-0000-0000-000000000001" {
		t.Fatalf("migrated Session Resource tenant UUIDs = %q", tenantUUIDs)
	}
	for _, removedColumn := range []string{"organization_id", "workspace_id", "session_id"} {
		var exists bool
		if err := database.QueryRowContext(ctx, `
			select exists (
				select 1 from information_schema.columns
				where table_schema = current_schema()
					and table_name = 'session_resources'
					and column_name = $1
			)
		`, removedColumn).Scan(&exists); err != nil {
			t.Fatalf("check removed Session Resource column %s: %v", removedColumn, err)
		}
		if exists {
			t.Fatalf("session_resources.%s still exists after migration", removedColumn)
		}
	}

	var skillResourceUUID, skillFileUUID, skillSource, skillFilename, skillKey, skillChecksum string
	var skillSize int64
	if err := database.QueryRowContext(ctx, `
		select cast(resource.uuid as text), cast(file.uuid as text),
			file.metadata->>'skill_source', file.filename, file.size_bytes,
			file.sha256, file.s3_key
		from session_resources resource
		join files file
			on file.uuid = resource.file_uuid
		join workspaces workspace
			on workspace.id = file.workspace_id
			and workspace.uuid = resource.workspace_uuid
		where resource.path = '/skills/migration-skill'
	`).Scan(
		&skillResourceUUID,
		&skillFileUUID,
		&skillSource,
		&skillFilename,
		&skillSize,
		&skillChecksum,
		&skillKey,
	); err != nil {
		t.Fatalf("load migrated Skill snapshot: %v", err)
	}
	if skillResourceUUID == skillFileUUID || skillSource != "custom" ||
		skillFilename != "migration-skill.zip" || skillSize != 128 ||
		skillChecksum != strings.Repeat("e", 64) || skillKey != "skills/migration-skill.zip" {
		t.Fatalf(
			"migrated Skill snapshot = resource %q file %q source %q filename %q size %d checksum %q key %q",
			skillResourceUUID,
			skillFileUUID,
			skillSource,
			skillFilename,
			skillSize,
			skillChecksum,
			skillKey,
		)
	}

	var fileCount int
	if err := database.QueryRowContext(ctx, `select count(*) from files`).Scan(&fileCount); err != nil {
		t.Fatalf("count migrated Files: %v", err)
	}
	if fileCount != 4 {
		t.Fatalf("files count = %d, want source, active and expired outputs, plus Skill snapshot", fileCount)
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

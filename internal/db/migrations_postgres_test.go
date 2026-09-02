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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/superduck-ai/yourbatis"
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

func TestEnvironmentWorkSessionUUIDMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}

	t.Run("backfills active and deleted Sessions and round trips", func(t *testing.T) {
		ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
		if _, err := provider.UpTo(ctx, 51); err != nil {
			t.Fatalf("migrate fixture database to 51: %v", err)
		}
		seedEnvironmentWorkMigrationSessions(t, ctx, database)
		if _, err := database.ExecContext(ctx, `
			insert into environment_work (
				uuid, external_id, organization_uuid, workspace_uuid,
				environment_uuid, environment_external_id, data
			) values
				(
					'52000000-0000-0000-0000-000000000101', 'work_migration_active',
					'52000000-0000-0000-0000-000000000001', '52000000-0000-0000-0000-000000000002',
					'52000000-0000-0000-0000-000000000003', 'env_migration_work',
					'{"type":"session","id":"sesn_migration_work_active"}'
				),
				(
					'52000000-0000-0000-0000-000000000102', 'work_migration_deleted',
					'52000000-0000-0000-0000-000000000001', '52000000-0000-0000-0000-000000000002',
					'52000000-0000-0000-0000-000000000003', 'env_migration_work',
					'{"type":"session","id":"sesn_migration_work_deleted"}'
				)
		`); err != nil {
			t.Fatalf("seed Environment Work: %v", err)
		}

		if _, err := provider.UpTo(ctx, 52); err != nil {
			t.Fatalf("migrate Environment Work to Session UUID: %v", err)
		}
		assertEnvironmentWorkSessionMigrationState(t, ctx, database)

		if _, err := provider.Down(ctx); err != nil {
			t.Fatalf("roll back Environment Work Session UUID migration: %v", err)
		}
		var activeData, deletedData string
		if err := database.QueryRowContext(ctx, `
			select cast(data as text) from environment_work where external_id = 'work_migration_active'
		`).Scan(&activeData); err != nil {
			t.Fatalf("load restored active Work data: %v", err)
		}
		if err := database.QueryRowContext(ctx, `
			select cast(data as text) from environment_work where external_id = 'work_migration_deleted'
		`).Scan(&deletedData); err != nil {
			t.Fatalf("load restored deleted-Session Work data: %v", err)
		}
		for name, restored := range map[string]string{"active": activeData, "deleted": deletedData} {
			var data map[string]string
			if err := json.Unmarshal([]byte(restored), &data); err != nil {
				t.Fatalf("decode restored %s Work data: %v", name, err)
			}
			if data["type"] != "session" || data["id"] != "sesn_migration_work_"+name {
				t.Fatalf("restored %s Work data = %#v", name, data)
			}
		}
		assertMigrationColumnExists(t, ctx, database, "environment_work", "data", true)
		assertMigrationColumnExists(t, ctx, database, "environment_work", "session_uuid", false)

		if _, err := provider.Up(ctx); err != nil {
			t.Fatalf("reapply Environment Work Session UUID migration: %v", err)
		}
		assertEnvironmentWorkSessionMigrationState(t, ctx, database)
	})

	invalidCases := []struct {
		name      string
		data      string
		wantError string
	}{
		{name: "non Session type", data: `{"type":"task","id":"sesn_migration_work_active"}`, wantError: "non-session or unsupported data"},
		{name: "non string Session ID", data: `{"type":"session","id":42}`, wantError: "non-session or unsupported data"},
		{name: "extra field", data: `{"type":"session","id":"sesn_migration_work_active","custom":true}`, wantError: "non-session or unsupported data"},
		{name: "unmapped Session", data: `{"type":"session","id":"sesn_missing"}`, wantError: "cannot be uniquely mapped"},
		{name: "non object", data: `[]`, wantError: "must be a JSON object"},
	}
	for _, test := range invalidCases {
		t.Run("rejects "+test.name, func(t *testing.T) {
			ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
			if _, err := provider.UpTo(ctx, 51); err != nil {
				t.Fatalf("migrate fixture database to 51: %v", err)
			}
			seedEnvironmentWorkMigrationSessions(t, ctx, database)
			if _, err := database.ExecContext(ctx, `
				insert into environment_work (
					uuid, external_id, organization_uuid, workspace_uuid,
					environment_uuid, environment_external_id, data
				) values (
					'52000000-0000-0000-0000-000000000103', 'work_migration_invalid',
					'52000000-0000-0000-0000-000000000001', '52000000-0000-0000-0000-000000000002',
					'52000000-0000-0000-0000-000000000003', 'env_migration_work', cast($1 as jsonb)
				)
			`, test.data); err != nil {
				t.Fatalf("seed invalid Environment Work: %v", err)
			}
			if _, err := provider.UpTo(ctx, 52); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("migration error = %v, want containing %q", err, test.wantError)
			}
			assertMigrationColumnExists(t, ctx, database, "environment_work", "data", true)
			assertMigrationColumnExists(t, ctx, database, "environment_work", "session_uuid", false)
		})
	}
}

func newIsolatedMigrationTestDatabase(
	t *testing.T,
	databaseURL string,
) (context.Context, *sql.DB, *goose.Provider) {
	t.Helper()
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open migration administration database: %v", err)
	}
	schema := "migration_environment_work_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	if _, err := adminPool.Exec(ctx, "create schema "+schema); err != nil {
		adminPool.Close()
		t.Fatalf("create isolated migration schema: %v", err)
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "drop schema "+schema+" cascade")
		adminPool.Close()
		t.Fatalf("parse migration database URL: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		_, _ = adminPool.Exec(ctx, "drop schema "+schema+" cascade")
		adminPool.Close()
		t.Fatalf("open isolated migration database: %v", err)
	}
	standardDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() {
		_ = standardDB.Close()
		pool.Close()
		_, _ = adminPool.Exec(context.Background(), "drop schema "+schema+" cascade")
		adminPool.Close()
	})
	return ctx, standardDB, newMigrationTestProvider(t, standardDB)
}

func seedEnvironmentWorkMigrationSessions(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		insert into sessions (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, deleted_at
		) values
			(
				'52000000-0000-0000-0000-000000000011', 'sesn_migration_work_active',
				'52000000-0000-0000-0000-000000000001', '52000000-0000-0000-0000-000000000002',
				'52000000-0000-0000-0000-000000000004', '52000000-0000-0000-0000-000000000003',
				'env_migration_work', '52000000-0000-0000-0000-000000000005', 'agent_migration_work',
				1, '{}', null
			),
			(
				'52000000-0000-0000-0000-000000000012', 'sesn_migration_work_deleted',
				'52000000-0000-0000-0000-000000000001', '52000000-0000-0000-0000-000000000002',
				'52000000-0000-0000-0000-000000000004', '52000000-0000-0000-0000-000000000003',
				'env_migration_work', '52000000-0000-0000-0000-000000000005', 'agent_migration_work',
				1, '{}', now()
			)
	`); err != nil {
		t.Fatalf("seed migration Sessions: %v", err)
	}
}

func assertEnvironmentWorkSessionMigrationState(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	rows, err := database.QueryContext(ctx, `
		select external_id, cast(session_uuid as text)
		from environment_work
		order by external_id
	`)
	if err != nil {
		t.Fatalf("load migrated Environment Work: %v", err)
	}
	defer rows.Close()
	got := make(map[string]string)
	for rows.Next() {
		var externalID, sessionUUID string
		if err := rows.Scan(&externalID, &sessionUUID); err != nil {
			t.Fatalf("scan migrated Environment Work: %v", err)
		}
		got[externalID] = sessionUUID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migrated Environment Work: %v", err)
	}
	want := map[string]string{
		"work_migration_active":  "52000000-0000-0000-0000-000000000011",
		"work_migration_deleted": "52000000-0000-0000-0000-000000000012",
	}
	if len(got) != len(want) {
		t.Fatalf("migrated Environment Work = %#v, want %#v", got, want)
	}
	for externalID, sessionUUID := range want {
		if got[externalID] != sessionUUID {
			t.Fatalf("migrated %s Session UUID = %q, want %q", externalID, got[externalID], sessionUUID)
		}
	}
	assertMigrationColumnExists(t, ctx, database, "environment_work", "data", false)
	assertMigrationColumnExists(t, ctx, database, "environment_work", "session_uuid", true)
	var nullable string
	if err := database.QueryRowContext(ctx, `
		select is_nullable
		from information_schema.columns
		where table_schema = current_schema()
			and table_name = 'environment_work'
			and column_name = 'session_uuid'
	`).Scan(&nullable); err != nil {
		t.Fatalf("check Environment Work Session nullability: %v", err)
	}
	if nullable != "NO" {
		t.Fatalf("environment_work.session_uuid is_nullable = %q, want NO", nullable)
	}
	var indexDefinition string
	if err := database.QueryRowContext(ctx, `
		select indexdef
		from pg_indexes
		where schemaname = current_schema()
			and indexname = 'environment_work_session_v1_idx'
	`).Scan(&indexDefinition); err != nil {
		t.Fatalf("load Environment Work Session index: %v", err)
	}
	if !strings.Contains(indexDefinition, "(workspace_uuid, session_uuid)") ||
		!strings.Contains(indexDefinition, "WHERE (deleted_at IS NULL)") {
		t.Fatalf("Environment Work Session index = %q", indexDefinition)
	}
}

func assertMigrationColumnExists(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	tableName string,
	columnName string,
	want bool,
) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `
		select exists (
			select 1
			from information_schema.columns
			where table_schema = current_schema()
				and table_name = $1
				and column_name = $2
		)
	`, tableName, columnName).Scan(&exists); err != nil {
		t.Fatalf("check migration column %s.%s: %v", tableName, columnName, err)
	}
	if exists != want {
		t.Fatalf("migration column %s.%s exists = %t, want %t", tableName, columnName, exists, want)
	}
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

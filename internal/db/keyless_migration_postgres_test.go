package db

import (
	"os"
	"testing"
)

func TestKeylessResourceCreatorsMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}

	for _, test := range []struct {
		name    string
		keyless bool
	}{
		{name: "rejects rollback without deleting keyless resources", keyless: true},
		{name: "preserves key references and removes untrusted runtime metadata"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
			if _, err := provider.UpTo(ctx, 54); err != nil {
				t.Fatalf("migrate fixture database to 54: %v", err)
			}
			seedEnvironmentWorkMigrationSessions(t, ctx, database)
			if _, err := database.ExecContext(ctx, `
				update sessions set metadata = '{"public":"keep","_oma_runtime_user_uuid":"untrusted"}';
				insert into deployments (
					external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
					environment_uuid, environment_external_id, agent_uuid, agent_external_id,
					agent_version, agent_snapshot, name, metadata
				)
				select 'depl_migration_keyless', organization_uuid, workspace_uuid, created_by_api_key_uuid,
					environment_uuid, environment_external_id, agent_uuid, agent_external_id,
					agent_version, agent_snapshot, 'Migration deployment', metadata
				from sessions where deleted_at is null
			`); err != nil {
				t.Fatalf("seed untrusted runtime metadata: %v", err)
			}
			if _, err := provider.UpTo(ctx, 55); err != nil {
				t.Fatalf("apply keyless resource migration: %v", err)
			}

			var preserved int
			if err := database.QueryRowContext(ctx, `
				select count(*) from (
					select metadata, created_by_api_key_uuid from sessions
					union all
					select metadata, created_by_api_key_uuid from deployments
				) resources
				where metadata = '{"public":"keep"}'::jsonb
				and created_by_api_key_uuid = '52000000-0000-0000-0000-000000000004'
			`).Scan(&preserved); err != nil || preserved != 3 {
				t.Fatalf("preserved resources = %d, error = %v; want 3", preserved, err)
			}
			var nullable int
			if err := database.QueryRowContext(ctx, `
				select count(*) from information_schema.columns
				where table_schema = current_schema()
				and table_name in (
					'files', 'skills', 'skill_versions', 'agents', 'environments', 'vaults',
					'memory_stores', 'webhook_endpoints', 'message_batches',
					'deployments', 'deployment_runs', 'sessions'
				)
				and column_name = 'created_by_api_key_uuid' and is_nullable = 'YES'
			`).Scan(&nullable); err != nil || nullable != 12 {
				t.Fatalf("nullable resource creators = %d, error = %v; want 12", nullable, err)
			}

			if test.keyless {
				if _, err := database.ExecContext(ctx, `
					update sessions set created_by_api_key_uuid = null where deleted_at is null
				`); err != nil {
					t.Fatalf("write keyless Session: %v", err)
				}
				if _, err := provider.Down(ctx); err == nil {
					t.Fatal("rollback accepted keyless resources")
				}
				var keyless int
				if err := database.QueryRowContext(ctx, `
					select count(*) from sessions where created_by_api_key_uuid is null
				`).Scan(&keyless); err != nil || keyless != 1 {
					t.Fatalf("keyless resources after rejected rollback = %d, error = %v; want 1", keyless, err)
				}
				return
			}
			if _, err := provider.Down(ctx); err != nil {
				t.Fatalf("roll back migration with genuine key references: %v", err)
			}
			if _, err := provider.UpTo(ctx, 55); err != nil {
				t.Fatalf("reapply keyless resource migration: %v", err)
			}
		})
	}
}

package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/yourbatis"
)

var legacyCreatorTables = []string{
	"agents", "deployment_runs", "deployments", "environments", "files",
	"memory_stores", "message_batches", "sessions", "skill_versions", "skills",
	"vaults", "webhook_endpoints", "filestore_filesystems", "vault_credentials",
}

func TestLegacyCreatorConstraintsMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 54); err != nil {
		t.Fatal(err)
	}
	seedLegacyCreatorSchema(t, ctx, database)
	seedLegacyCreatorRuntimeResources(t, ctx, database)
	// Simulate the withdrawn migration 55: Goose must not replay today's file.
	if _, err := database.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (55, true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 55); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres, yourbatis.WithDatabaseID("postgres"))}
	now := time.Now().UTC()
	agent := Agent{
		UUID: "56000000-0000-0000-0000-000000000001", ExternalID: "agent_keyless_upgrade",
		WorkspaceUUID: "52000000-0000-0000-0000-000000000002", Name: "Keyless upgrade",
		Model: json.RawMessage(`"kimi-2.5"`), MCPServers: json.RawMessage(`[]`),
		Metadata: json.RawMessage(`{}`), Skills: json.RawMessage(`[]`), Tools: json.RawMessage(`[]`),
		CreatedAt: now, UpdatedAt: now,
	}
	t.Run("failure legacy creator constraint rejects keyless agent", func(t *testing.T) {
		_, err := store.CreateAgent(ctx, agent, "agentver_keyless_upgrade")
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23514" || postgresError.ConstraintName != "agents_creator_check" {
			t.Fatalf("create agent error = %v, want agents_creator_check violation", err)
		}
	})

	if _, err := provider.UpTo(ctx, 56); err != nil {
		t.Fatalf("apply legacy creator compatibility migration: %v", err)
	}
	t.Run("success keyless agent and version are persisted", func(t *testing.T) {
		created, err := store.CreateAgent(ctx, agent, "agentver_keyless_upgrade")
		if err != nil || created.CreatedByAPIKeyUUID != "" {
			t.Fatalf("create keyless agent: %v, actor %q", err, created.CreatedByAPIKeyUUID)
		}
		if _, err := store.GetAgentVersion(ctx, agent.WorkspaceUUID, agent.ExternalID, 1); err != nil {
			t.Fatalf("read created agent version: %v", err)
		}
	})
	t.Run("success preserves old actors and safely migrates runtime users", func(t *testing.T) {
		var migrated int
		if err := database.QueryRowContext(ctx, `
			SELECT count(*) FROM (
				SELECT metadata, created_by_user_uuid, created_by_api_key_uuid FROM sessions WHERE deleted_at IS NULL
				UNION ALL SELECT metadata, created_by_user_uuid, created_by_api_key_uuid FROM deployments
			) resources
			WHERE metadata = '{"public":"keep","_oma_runtime_user_uuid":"56000000-0000-0000-0000-000000000002"}'::jsonb
			AND created_by_user_uuid = '56000000-0000-0000-0000-000000000002'
			AND created_by_api_key_uuid IS NULL
		`).Scan(&migrated); err != nil || migrated != 2 {
			t.Fatalf("migrated runtime identities = %d, error = %v; want 2", migrated, err)
		}
		var preserved bool
		if err := database.QueryRowContext(ctx, `
			SELECT metadata = '{"public":"keep"}'::jsonb
				AND created_by_api_key_uuid = '52000000-0000-0000-0000-000000000004'
			FROM sessions WHERE deleted_at IS NOT NULL
		`).Scan(&preserved); err != nil || !preserved {
			t.Fatalf("legacy key actor preserved and spoofed metadata removed = %t: %v", preserved, err)
		}
	})
	for _, table := range legacyCreatorTables {
		var constraints int
		if err := database.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_constraint WHERE connamespace = current_schema()::regnamespace AND conname = $1
		`, table+"_creator_check").Scan(&constraints); err != nil || constraints != 0 {
			t.Fatalf("remaining %s creator constraints = %d: %v", table, constraints, err)
		}
		assertMigrationColumnExists(t, ctx, database, table, "created_by_user_uuid", true)
	}
	var checks int
	if err := database.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_constraint WHERE connamespace = current_schema()::regnamespace AND conname = 'agents_current_version_positive'
	`).Scan(&checks); err != nil || checks != 1 {
		t.Fatalf("unrelated constraints preserved = %d: %v", checks, err)
	}
	// Down cannot restore the discarded draft contract. Reapplying must not trust
	// the retained legacy column over runtime identities written after migration.
	if _, err := database.ExecContext(ctx, `
		UPDATE sessions SET metadata = '{"_oma_runtime_user_uuid":"56000000-0000-0000-0000-000000000003"}' WHERE deleted_at IS NULL
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 56); err != nil {
		t.Fatal(err)
	}
	assertMigrationRuntimeUser(t, ctx, database, "56000000-0000-0000-0000-000000000003")
}

func TestLegacyCreatorMigrationPreservesCurrentRuntimeIdentity(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 55); err != nil {
		t.Fatal(err)
	}
	seedEnvironmentWorkMigrationSessions(t, ctx, database)
	if _, err := database.ExecContext(ctx, `
		UPDATE sessions SET created_by_api_key_uuid = NULL,
			metadata = '{"_oma_runtime_user_uuid":"56000000-0000-0000-0000-000000000004"}'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 56); err != nil {
		t.Fatal(err)
	}
	assertMigrationRuntimeUser(t, ctx, database, "56000000-0000-0000-0000-000000000004")
	for _, table := range legacyCreatorTables {
		assertMigrationColumnExists(t, ctx, database, table, "created_by_user_uuid", false)
	}
}

func seedLegacyCreatorSchema(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, table := range legacyCreatorTables {
		comparison := "= 1"
		if table == "filestore_filesystems" || table == "vault_credentials" {
			comparison = "<= 1"
		}
		// Table identifiers come only from the fixed fixture whitelist above.
		statement := fmt.Sprintf(`ALTER TABLE %s
			ALTER COLUMN created_by_api_key_uuid DROP NOT NULL,
			ADD COLUMN created_by_user_uuid uuid,
			ADD CONSTRAINT %s_creator_check CHECK (num_nonnulls(created_by_api_key_uuid, created_by_user_uuid) %s)`, table, table, comparison)
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed legacy schema for %s: %v", table, err)
		}
	}
}

func seedLegacyCreatorRuntimeResources(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	seedEnvironmentWorkMigrationSessions(t, ctx, database)
	if _, err := database.ExecContext(ctx, `
		UPDATE sessions SET metadata = '{"public":"keep","_oma_runtime_user_uuid":"untrusted-client-value"}';
		UPDATE sessions SET created_by_api_key_uuid = NULL,
			created_by_user_uuid = '56000000-0000-0000-0000-000000000002' WHERE deleted_at IS NULL;
		INSERT INTO deployments (
			external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid, created_by_user_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, name, metadata
		)
		SELECT 'depl_legacy_creator', organization_uuid, workspace_uuid, created_by_api_key_uuid, created_by_user_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, 'Legacy creator', metadata
		FROM sessions WHERE deleted_at IS NULL
	`); err != nil {
		t.Fatalf("seed legacy runtime resources: %v", err)
	}
}

func assertMigrationRuntimeUser(t *testing.T, ctx context.Context, database *sql.DB, want string) {
	t.Helper()
	var got string
	if err := database.QueryRowContext(ctx, `
		SELECT metadata->>'_oma_runtime_user_uuid' FROM sessions WHERE deleted_at IS NULL
	`).Scan(&got); err != nil || got != want {
		t.Fatalf("runtime user = %q, want %q: %v", got, want, err)
	}
}

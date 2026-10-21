//go:build integration

package db

import (
	"io/fs"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestOrganizationLLMProviderMigrationFromMain(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
	migrationFS, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	mainProvider, err := goose.NewProvider(
		goose.DialectPostgres, database, migrationFS,
		goose.WithDisableGlobalRegistry(true),
		goose.WithLogger(goose.NopLogger()),
		goose.WithExcludeNames([]string{"00094_organization_llm_provider.sql"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mainProvider.UpTo(ctx, 91); err != nil {
		t.Fatalf("migrate to main schema: %v", err)
	}
	if err := runGooseMigrations(ctx, database); err != nil {
		t.Fatalf("upgrade main schema with organization provider: %v", err)
	}
	var nullable string
	if err := database.QueryRowContext(ctx, `
		select is_nullable from information_schema.columns
		where table_schema = current_schema() and table_name = 'llm_providers' and column_name = 'workspace_uuid'
	`).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	if nullable != "YES" {
		t.Fatal("organization providers require a nullable workspace_uuid")
	}
	var indexExists bool
	if err := database.QueryRowContext(ctx, `
		select exists(select 1 from pg_indexes
		where schemaname = current_schema() and indexname = 'llm_providers_organization_default_key')
	`).Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if !indexExists {
		t.Fatal("organization provider uniqueness index is missing")
	}
}

func TestOrganizationLLMProviderMigrationPreservesWorkspaceReferences(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 91); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
 INSERT INTO llm_providers (external_id,organization_uuid,workspace_uuid,name,slug,base_url,api_key_last4,model_ids,ciphertext,nonce,wrapped_dek,format_version,key_provider,key_version) VALUES
 ('llmprov_old','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','Legacy','organization','https://legacy.example.com','0001','["model"]','x','x','x',1,'local',1),
 ('llmprov_occupied','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','Occupied','organization-2','https://other.example.com','0002','[]','x','x','x',1,'local',1);
 INSERT INTO agents (external_id,name,model,workspace_uuid,created_by_api_key_uuid) VALUES
 ('agent_legacy','Legacy','{"id":"organization/model","speed":"fast"}','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000009'),
 ('agent_other','Other','"organization/model"','bb000000-0000-4000-8000-000000000003','bb000000-0000-4000-8000-000000000009');
 INSERT INTO code_sessions (external_id,organization_uuid,workspace_uuid,session_uuid,environment_uuid,session_external_id,environment_external_id,model,worker_external_metadata) VALUES
 ('cse_legacy','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000012','bb000000-0000-4000-8000-000000000011','sesn_object','env_test','organization/model','{"model":"organization/model","extra":true}');
 `); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 94); err != nil {
		t.Fatal(err)
	}
	assertMigratedValues(t, database, `SELECT external_id,slug FROM llm_providers`, map[string]string{"llmprov_old": "organization-3", "llmprov_occupied": "organization-2"})
	assertMigratedValues(t, database, `SELECT external_id,model::text FROM agents`, map[string]string{"agent_legacy": `{"id": "organization-3/model", "speed": "fast"}`, "agent_other": `"organization/model"`})
	assertMigratedValues(t, database, `SELECT external_id,model FROM code_sessions`, map[string]string{"cse_legacy": "organization-3/model"})
	assertMigratedValues(t, database, `SELECT external_id,worker_external_metadata::text FROM code_sessions`, map[string]string{"cse_legacy": `{"extra": true, "model": "organization-3/model"}`})
	if _, err := database.ExecContext(ctx, `UPDATE llm_providers SET slug='organization' WHERE external_id='llmprov_old'`); err == nil {
		t.Fatal("reserved workspace slug was accepted")
	}
	if _, err := provider.DownTo(ctx, 91); err != nil {
		t.Fatal(err)
	}
	assertMigratedValues(t, database, `SELECT external_id,slug FROM llm_providers`, map[string]string{"llmprov_old": "organization-3", "llmprov_occupied": "organization-2"})
}

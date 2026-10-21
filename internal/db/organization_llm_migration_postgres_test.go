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

 INSERT INTO agent_versions (external_id,agent_external_id,agent_uuid,agent_created_at,agent_updated_at,workspace_uuid,version,model,name) VALUES
 ('version_object','agent_legacy',gen_random_uuid(),now(),now(),'bb000000-0000-4000-8000-000000000002',1,'{"id":"organization/model","speed":"fast"}','Object'),
 ('version_string','agent_legacy',gen_random_uuid(),now(),now(),'bb000000-0000-4000-8000-000000000002',2,'"organization/model"','String'),
 ('version_other_workspace','agent_other',gen_random_uuid(),now(),now(),'bb000000-0000-4000-8000-000000000003',1,'"organization/model"','Other workspace'),
 ('version_other_org','agent_other',gen_random_uuid(),now(),now(),'bb000000-0000-4000-8000-000000000005',1,'"organization/model"','Other org'),
 ('version_unrelated','agent_legacy',gen_random_uuid(),now(),now(),'bb000000-0000-4000-8000-000000000002',3,'"organization/missing"','Unrelated');
 INSERT INTO sessions (external_id,agent_uuid,organization_uuid,workspace_uuid,environment_uuid,agent_version,agent_snapshot,environment_external_id,agent_external_id) VALUES
 ('session_object',gen_random_uuid(),'bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002',gen_random_uuid(),1,'{"model":{"id":"organization/model","speed":"fast"},"keep":true}','env_test','agent_legacy'),
 ('session_string',gen_random_uuid(),'bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002',gen_random_uuid(),1,'{"model":"organization/model","keep":true}','env_test','agent_legacy'),
 ('session_other_workspace',gen_random_uuid(),'bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000003',gen_random_uuid(),1,'{"model":"organization/model","keep":true}','env_test','agent_other'),
 ('session_other_org',gen_random_uuid(),'bb000000-0000-4000-8000-000000000004','bb000000-0000-4000-8000-000000000005',gen_random_uuid(),1,'{"model":"organization/model","keep":true}','env_test','agent_other'),
 ('session_unrelated',gen_random_uuid(),'bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002',gen_random_uuid(),1,'{"model":"organization/missing","keep":true}','env_test','agent_legacy');
 INSERT INTO message_batches (uuid,external_id,organization_uuid,workspace_uuid,created_by_api_key_uuid,api_variant,request_count,expires_at) VALUES
 ('bb000000-0000-4000-8000-000000000021','batch_main','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002',gen_random_uuid(),'stable',4,now()+interval '1 day'),
 ('bb000000-0000-4000-8000-000000000022','batch_other_workspace','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000003',gen_random_uuid(),'stable',1,now()+interval '1 day'),
 ('bb000000-0000-4000-8000-000000000023','batch_other_org','bb000000-0000-4000-8000-000000000004','bb000000-0000-4000-8000-000000000005',gen_random_uuid(),'stable',1,now()+interval '1 day');
 INSERT INTO message_batch_requests (external_id,workspace_uuid,message_batch_uuid,request_index,custom_id,params,status) VALUES
 ('request_queued','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000021',0,'queued','{"model":"organization/model","max_tokens":32}','queued'),
 ('request_in_flight','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000021',1,'in_flight','{"model":"organization/model","max_tokens":32}','in_flight'),
 ('request_completed','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000021',2,'completed','{"model":"organization/model","max_tokens":32}','succeeded'),
 ('request_unrelated','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000021',3,'unrelated','{"model":"organization/missing","max_tokens":32}','queued'),
 ('request_other_workspace','bb000000-0000-4000-8000-000000000003','bb000000-0000-4000-8000-000000000022',0,'other_workspace','{"model":"organization/model","max_tokens":32}','queued'),
 ('request_other_org','bb000000-0000-4000-8000-000000000005','bb000000-0000-4000-8000-000000000023',0,'other_org','{"model":"organization/model","max_tokens":32}','queued');
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
	assertMigratedValues(t, database, `SELECT external_id,model::text FROM agent_versions`, map[string]string{
		"version_object":          `{"id": "organization-3/model", "speed": "fast"}`,
		"version_string":          `"organization-3/model"`,
		"version_other_workspace": `"organization/model"`,
		"version_other_org":       `"organization/model"`,
		"version_unrelated":       `"organization/missing"`,
	})
	assertMigratedValues(t, database, `SELECT external_id,agent_snapshot::text FROM sessions`, map[string]string{
		"session_object":          `{"keep": true, "model": {"id": "organization-3/model", "speed": "fast"}}`,
		"session_string":          `{"keep": true, "model": "organization-3/model"}`,
		"session_other_workspace": `{"keep": true, "model": "organization/model"}`,
		"session_other_org":       `{"keep": true, "model": "organization/model"}`,
		"session_unrelated":       `{"keep": true, "model": "organization/missing"}`,
	})
	assertMigratedValues(t, database, `SELECT external_id,params::text FROM message_batch_requests`, map[string]string{
		"request_queued":          `{"model": "organization-3/model", "max_tokens": 32}`,
		"request_in_flight":       `{"model": "organization-3/model", "max_tokens": 32}`,
		"request_completed":       `{"model": "organization/model", "max_tokens": 32}`,
		"request_unrelated":       `{"model": "organization/missing", "max_tokens": 32}`,
		"request_other_workspace": `{"model": "organization/model", "max_tokens": 32}`,
		"request_other_org":       `{"model": "organization/model", "max_tokens": 32}`,
	})

	if _, err := database.ExecContext(ctx, `UPDATE llm_providers SET slug='organization' WHERE external_id='llmprov_old'`); err == nil {
		t.Fatal("reserved workspace slug was accepted")
	}
	if _, err := provider.DownTo(ctx, 91); err != nil {
		t.Fatal(err)
	}
	assertMigratedValues(t, database, `SELECT external_id,slug FROM llm_providers`, map[string]string{"llmprov_old": "organization-3", "llmprov_occupied": "organization-2"})
}

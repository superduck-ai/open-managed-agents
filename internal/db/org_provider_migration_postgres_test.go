//go:build integration

package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
	"github.com/superduck-ai/yourbatis"
)

const (
	providerMigrationOrg             = "ef000000-0000-4000-8000-000000000001"
	providerMigrationWorkspace       = "ef000000-0000-4000-8000-000000000002"
	providerMigrationSecondWorkspace = "ef000000-0000-4000-8000-000000000003"
	providerMigrationOtherOrg        = "ef000000-0000-4000-8000-000000000009"
	providerMigrationAccount         = "ef000000-0000-4000-8000-000000000011"
)

type providerMigrationFixture struct {
	uuid         string
	id           string
	slug         string
	organization string
	workspace    string
	models       string
	envelope     secrets.Envelope
}

func TestOrganizationOwnedLLMProvidersMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is required for isolated PostgreSQL migration validation")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := migrations.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	seedProviderMigrationScopes(t, ctx, database)
	keyProvider, err := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{31}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := secrets.NewService(keyProvider)
	fixtures := seedProviderMigrationProviders(t, ctx, database, service)
	seedProviderMigrationReferences(t, ctx, database)
	if _, err := migrations.UpTo(ctx, 118); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	if _, err := store.GetLLMProvider(ctx, providerMigrationOtherOrg, "llmprov_local"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-organization provider lookup = %v", err)
	}
	if err := store.DeleteLLMProvider(ctx, providerMigrationOtherOrg, "llmprov_local"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-organization provider delete = %v", err)
	}
	for _, fixture := range fixtures {
		provider, err := store.GetLLMProvider(ctx, fixture.organization, fixture.id)
		if err != nil {
			t.Fatal(err)
		}
		if provider.UUID != fixture.uuid || provider.Slug != fixture.slug || provider.SecretWorkspaceUUID != fixture.workspace || provider.CreatedByAccountUUID != "" {
			t.Fatalf("provider identity or historical attribution changed: %s", fixture.id)
		}
		if provider.SecretEnvelope == nil || !bytes.Equal(provider.SecretEnvelope.Ciphertext, fixture.envelope.Ciphertext) || !bytes.Equal(provider.SecretEnvelope.WrappedDEK, fixture.envelope.WrappedDEK) || !bytes.Equal(provider.SecretEnvelope.Nonce, fixture.envelope.Nonce) {
			t.Fatalf("provider envelope changed: %s", fixture.id)
		}
		bindingWorkspace := provider.SecretWorkspaceUUID
		if bindingWorkspace == "" {
			bindingWorkspace = OrganizationLLMProviderSecretWorkspace
		}
		plaintext, err := service.Open(ctx, secrets.Binding{OrganizationUUID: fixture.organization, WorkspaceUUID: bindingWorkspace, VaultExternalID: "llm_provider", CredentialExternalID: fixture.id}, *provider.SecretEnvelope)
		if err != nil || string(plaintext) != "migration-test-key" {
			t.Fatalf("migrated provider key cannot decrypt: %s, %v", fixture.id, err)
		}
		clear(plaintext)
	}
	providers, err := store.ListLLMProviders(ctx, providerMigrationOrg)
	if err != nil || len(providers) != 4 {
		t.Fatalf("organization provider count = %d, %v", len(providers), err)
	}
	assertProviderMigrationReferences(t, ctx, database)
	assertProviderMigrationCreator(t, ctx, store, service, providers[0])
	assertProviderMigrationInventory(t, ctx, store)
	if _, err := migrations.UpTo(ctx, 118); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if _, err := migrations.Down(ctx); err == nil {
		t.Fatal("unsafe ownership rollback succeeded")
	}
}

func seedProviderMigrationScopes(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
INSERT INTO organizations (uuid, name) VALUES
('ef000000-0000-4000-8000-000000000001','Migration'),
('ef000000-0000-4000-8000-000000000009','Other');
INSERT INTO workspaces (uuid, external_id, organization_uuid, name) VALUES
('ef000000-0000-4000-8000-000000000002','wrkspc_migration','ef000000-0000-4000-8000-000000000001','First'),
('ef000000-0000-4000-8000-000000000003','wrkspc_second','ef000000-0000-4000-8000-000000000001','Second');
INSERT INTO users (uuid, external_id, organization_uuid, email, name, role) VALUES
('ef000000-0000-4000-8000-000000000011','user_global_creator','ef000000-0000-4000-8000-000000000009','creator@example.test','Global Creator','user');`)
	if err != nil {
		t.Fatal(err)
	}
}

func seedProviderMigrationProviders(t *testing.T, ctx context.Context, database *sql.DB, service *secrets.Service) []providerMigrationFixture {
	t.Helper()
	fixtures := []providerMigrationFixture{
		{uuid: "ef000000-0000-4000-8000-000000000021", id: "llmprov_local", slug: "local", organization: providerMigrationOrg, workspace: providerMigrationWorkspace, models: `[{"id":"shared"},{"id":"vendor/model"},{"id":"organization/global"},{"id":"ambiguous"}]`},
		{uuid: "ef000000-0000-4000-8000-000000000022", id: "llmprov_team", slug: "team", organization: providerMigrationOrg, workspace: providerMigrationSecondWorkspace, models: `[{"id":"shared"}]`},
		{uuid: "ef000000-0000-4000-8000-000000000023", id: "llmprov_org", slug: "organization", organization: providerMigrationOrg, models: `[{"id":"shared"},{"id":"global"},{"id":"ambiguous"}]`},
		{uuid: "ef000000-0000-4000-8000-000000000024", id: "llmprov_echo", slug: "echo", organization: providerMigrationOrg, workspace: providerMigrationWorkspace, models: `[{"id":"ambiguous"}]`},
		{uuid: "ef000000-0000-4000-8000-000000000025", id: "llmprov_foreign", slug: "foreign", organization: providerMigrationOtherOrg, workspace: providerMigrationSecondWorkspace, models: `[{"id":"shared"}]`},
	}
	for i := range fixtures {
		fixture := &fixtures[i]
		workspace := fixture.workspace
		if workspace == "" {
			workspace = OrganizationLLMProviderSecretWorkspace
		}
		envelope, err := service.Seal(ctx, secrets.Binding{OrganizationUUID: fixture.organization, WorkspaceUUID: workspace, VaultExternalID: "llm_provider", CredentialExternalID: fixture.id}, []byte("migration-test-key"))
		if err != nil {
			t.Fatal(err)
		}
		fixture.envelope = envelope
		_, err = database.ExecContext(ctx, `INSERT INTO llm_providers
(uuid,external_id,organization_uuid,workspace_uuid,name,slug,base_url,api_key_last4,models,ciphertext,nonce,wrapped_dek,format_version,key_provider,key_version)
VALUES ($1,$2,$3,$4,$5,$6,'https://models.example.test','test',$7,$8,$9,$10,$11,$12,$13)`,
			fixture.uuid, fixture.id, fixture.organization, nullableString(fixture.workspace), fixture.id, fixture.slug, fixture.models,
			envelope.Ciphertext, envelope.Nonce, envelope.WrappedDEK, envelope.FormatVersion, envelope.KeyProvider, envelope.KeyVersion)
		if err != nil {
			t.Fatal(err)
		}
	}
	return fixtures
}

func seedProviderMigrationReferences(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
INSERT INTO agents (external_id,name,model,multiagent,workspace_uuid,created_by_api_key_uuid) VALUES
('agent_migration','Migration','{"id":"shared","speed":"fast"}','{"advisor":{"model":"global"},"agents":[{"model":"vendor/model","multiagent":{"advisor":{"model":"shared"}}}],"subagents":{"predefined_agents":[{"model":"shared"}]},"workflows":{"predefined_agents":[{"model":"ambiguous"}]}}','ef000000-0000-4000-8000-000000000002',gen_random_uuid());
INSERT INTO agent_versions (external_id,agent_external_id,agent_uuid,agent_created_at,agent_updated_at,workspace_uuid,version,model,name,multiagent)
SELECT 'agentver_migration',external_id,uuid,created_at,updated_at,workspace_uuid,1,'"vendor/model"',name,multiagent FROM agents;
INSERT INTO sessions (external_id,agent_uuid,organization_uuid,workspace_uuid,environment_uuid,agent_version,agent_snapshot,environment_external_id,agent_external_id)
SELECT 'session_migration',uuid,'ef000000-0000-4000-8000-000000000001',workspace_uuid,gen_random_uuid(),1,jsonb_build_object('model',model,'multiagent',multiagent,'metadata',jsonb_build_object('model','shared')),'env_test',external_id FROM agents;
INSERT INTO session_threads (external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,agent_snapshot)
SELECT external_id,organization_uuid,workspace_uuid,uuid,external_id,agent_snapshot FROM sessions;
INSERT INTO deployments (external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,name)
SELECT external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,external_id FROM sessions;
INSERT INTO deployment_runs (external_id,organization_uuid,workspace_uuid,deployment_uuid,deployment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,session_external_id,trigger_type)
SELECT external_id,organization_uuid,workspace_uuid,uuid,external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,external_id,'manual' FROM deployments;
INSERT INTO code_sessions (external_id,organization_uuid,workspace_uuid,session_uuid,environment_uuid,session_external_id,environment_external_id,model,worker_external_metadata)
SELECT refs.external_id,refs.organization_uuid,refs.workspace_uuid,gen_random_uuid(),gen_random_uuid(),'session_migration','env_test',refs.model,jsonb_build_object('model',refs.model) FROM (VALUES
('code_ambiguous','ef000000-0000-4000-8000-000000000001'::uuid,'ef000000-0000-4000-8000-000000000002'::uuid,'ambiguous'),
('code_unknown','ef000000-0000-4000-8000-000000000001'::uuid,'ef000000-0000-4000-8000-000000000002'::uuid,'missing'),
('code_other_tenant','ef000000-0000-4000-8000-000000000009'::uuid,'ef000000-0000-4000-8000-000000000002'::uuid,'shared'),
('code_first','ef000000-0000-4000-8000-000000000001'::uuid,'ef000000-0000-4000-8000-000000000002'::uuid,'shared'),
('code_second','ef000000-0000-4000-8000-000000000001'::uuid,'ef000000-0000-4000-8000-000000000003'::uuid,'shared'),
('code_slash','ef000000-0000-4000-8000-000000000001'::uuid,'ef000000-0000-4000-8000-000000000002'::uuid,'vendor/model'),
('code_qualified','ef000000-0000-4000-8000-000000000001'::uuid,'ef000000-0000-4000-8000-000000000002'::uuid,'organization/global'),
('code_fallback','ef000000-0000-4000-8000-000000000001'::uuid,'ef000000-0000-4000-8000-000000000003'::uuid,'global')
) AS refs(external_id,organization_uuid,workspace_uuid,model);
INSERT INTO message_batches (uuid,external_id,organization_uuid,workspace_uuid,created_by_api_key_uuid,api_variant,request_count,expires_at)
VALUES ('ef000000-0000-4000-8000-000000000031','batch_migration','ef000000-0000-4000-8000-000000000001','ef000000-0000-4000-8000-000000000002',gen_random_uuid(),'stable',3,now()+interval '1 day');
INSERT INTO message_batch_requests (external_id,workspace_uuid,message_batch_uuid,request_index,custom_id,params,status) VALUES
('request_queued','ef000000-0000-4000-8000-000000000002','ef000000-0000-4000-8000-000000000031',0,'queued','{"model":"shared"}','queued'),
('request_inflight','ef000000-0000-4000-8000-000000000002','ef000000-0000-4000-8000-000000000031',1,'inflight','{"model":"vendor/model"}','in_flight'),
('request_completed','ef000000-0000-4000-8000-000000000002','ef000000-0000-4000-8000-000000000031',2,'completed','{"model":"shared"}','succeeded');`)
	if err != nil {
		t.Fatal(err)
	}
}

func assertProviderMigrationReferences(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	want := map[string]string{"code_ambiguous": "ambiguous", "code_unknown": "missing", "code_other_tenant": "shared", "code_first": "local/shared", "code_second": "team/shared", "code_slash": "local/vendor/model", "code_qualified": "organization/global", "code_fallback": "organization/global"}
	assertMigratedValues(t, database, `SELECT external_id, model FROM code_sessions`, want)
	assertMigratedValues(t, database, `SELECT external_id, worker_external_metadata->>'model' FROM code_sessions`, want)
	assertMigratedValues(t, database, `SELECT external_id, params->>'model' FROM message_batch_requests`, map[string]string{"request_queued": "local/shared", "request_inflight": "local/vendor/model", "request_completed": "shared"})
	for _, query := range []string{
		`SELECT model->>'id' FROM agents`,
		`SELECT multiagent#>>'{subagents,predefined_agents,0,model}' FROM agents`,
		`SELECT multiagent#>>'{agents,0,multiagent,advisor,model}' FROM agent_versions`,
		`SELECT agent_snapshot#>>'{model,id}' FROM sessions`,
		`SELECT agent_snapshot#>>'{model,id}' FROM session_threads`,
		`SELECT agent_snapshot#>>'{model,id}' FROM deployments`,
		`SELECT agent_snapshot#>>'{model,id}' FROM deployment_runs`,
	} {
		var model string
		if err := database.QueryRowContext(ctx, query).Scan(&model); err != nil || model != "local/shared" {
			t.Fatalf("%s = %q, %v", query, model, err)
		}
	}
	var metadata, advisor, roster, ambiguous string
	if err := database.QueryRowContext(ctx, `SELECT agent_snapshot#>>'{metadata,model}',agent_snapshot#>>'{multiagent,advisor,model}',agent_snapshot#>>'{multiagent,agents,0,model}',agent_snapshot#>>'{multiagent,workflows,predefined_agents,0,model}' FROM sessions`).Scan(&metadata, &advisor, &roster, &ambiguous); err != nil || metadata != "shared" || advisor != "organization/global" || roster != "local/vendor/model" || ambiguous != "ambiguous" {
		t.Fatalf("snapshot preservation/canonicalization = %q %q %q %q, %v", metadata, advisor, roster, ambiguous, err)
	}
}

func assertProviderMigrationCreator(t *testing.T, ctx context.Context, store *DB, service *secrets.Service, original LLMProvider) {
	t.Helper()
	provider := original
	provider.UUID = "ef000000-0000-4000-8000-000000000041"
	provider.ExternalID = "llmprov_owned"
	provider.Slug = "owned"
	provider.SecretWorkspaceUUID = ""
	provider.CreatedByAccountUUID = providerMigrationAccount
	envelope, err := service.Seal(ctx, secrets.Binding{OrganizationUUID: provider.OrganizationUUID, WorkspaceUUID: OrganizationLLMProviderSecretWorkspace, VaultExternalID: "llm_provider", CredentialExternalID: provider.ExternalID}, []byte("owned-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	provider.SecretEnvelope = &envelope
	created, err := store.CreateLLMProvider(ctx, provider)
	if err != nil || created.CreatedByName != "Global Creator" || created.CreatedByAccountUUID != providerMigrationAccount {
		t.Fatalf("creator projection = %q %q, %v", created.CreatedByName, created.CreatedByAccountUUID, err)
	}
	created.CreatedByAccountUUID = "ef000000-0000-4000-8000-000000000042"
	created.SecretWorkspaceUUID = providerMigrationWorkspace
	updated, err := store.UpdateLLMProvider(ctx, created)
	if err != nil || updated.CreatedByAccountUUID != providerMigrationAccount || updated.SecretWorkspaceUUID != "" {
		t.Fatalf("immutable creator/binding changed: %v", err)
	}
}

func assertProviderMigrationInventory(t *testing.T, ctx context.Context, store *DB) {
	t.Helper()
	entries, err := store.ListSecretMigrationPage(ctx, "llm_providers", "")
	if err != nil || len(entries) != 6 {
		t.Fatalf("secret inventory count = %d, %v", len(entries), err)
	}
	for _, entry := range entries {
		if entry.WorkspaceUUID == "" {
			t.Fatal("secret inventory lost binding")
		}
		wrong := entry
		wrong.OrganizationUUID = "ef000000-0000-4000-8000-000000000099"
		if err := store.SaveMigratedSecret(ctx, "llm_providers", wrong, entry.Document); !errors.Is(err, ErrVersionConflict) {
			t.Fatalf("foreign secret CAS = %v", err)
		}
		if err := store.SaveMigratedSecret(ctx, "llm_providers", entry, entry.Document); err != nil {
			t.Fatalf("secret CAS cannot match migrated binding: %v", err)
		}
	}
}

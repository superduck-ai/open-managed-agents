//go:build integration

package db

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestMultipleOrganizationLLMProvidersMigrationAndScope(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := migrations.UpTo(ctx, 104); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
 INSERT INTO llm_providers (uuid, external_id, organization_uuid, workspace_uuid, name, slug, base_url, api_key_last4, models, ciphertext, nonce, wrapped_dek, format_version, key_provider, key_version) VALUES
 ('aa000000-0000-4000-8000-000000000011','provider_first','aa000000-0000-4000-8000-000000000001','aa000000-0000-4000-8000-000000000002','First','shared','https://example.test','test','[{"id":"model"}]','x','x','x',1,'local',1),
 ('aa000000-0000-4000-8000-000000000012','provider_second','aa000000-0000-4000-8000-000000000001','aa000000-0000-4000-8000-000000000003','Second','shared','https://example.test','test','[{"id":"model"}]','x','x','x',1,'local',1),
 ('aa000000-0000-4000-8000-000000000013','provider_legacy','aa000000-0000-4000-8000-000000000001',NULL,'Legacy','organization','https://example.test','test','[{"id":"model"}]','x','x','x',1,'local',1);
 INSERT INTO agents (external_id,name,model,workspace_uuid,created_by_api_key_uuid) VALUES
 ('agent_first','First','"shared/model"','aa000000-0000-4000-8000-000000000002',gen_random_uuid()),
 ('agent_second','Second','{"id":"shared/model","speed":"fast"}','aa000000-0000-4000-8000-000000000003',gen_random_uuid());
 `); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
 UPDATE agents SET multiagent='{"advisor":{"model":"shared/model"},"subagents":{"predefined_agents":[{"model":{"id":"shared/model"}}]}}' WHERE external_id='agent_second';
 INSERT INTO sessions (external_id,agent_uuid,organization_uuid,workspace_uuid,environment_uuid,agent_version,agent_snapshot,environment_external_id,agent_external_id)
 VALUES ('session_second',gen_random_uuid(),'aa000000-0000-4000-8000-000000000001','aa000000-0000-4000-8000-000000000003',gen_random_uuid(),1,'{"model":"shared/model","multiagent":{"advisor":{"model":"shared/model"}}}','env_test','agent_second');
 INSERT INTO session_threads (external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,agent_snapshot)
 SELECT external_id,organization_uuid,workspace_uuid,uuid,external_id,agent_snapshot FROM sessions;
 INSERT INTO deployments (external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,name)
 SELECT external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,external_id FROM sessions;
 INSERT INTO deployment_runs (external_id,organization_uuid,workspace_uuid,deployment_uuid,deployment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,session_external_id,trigger_type)
 SELECT external_id,organization_uuid,workspace_uuid,uuid,external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,external_id,'manual' FROM deployments;
 INSERT INTO code_sessions (external_id,organization_uuid,workspace_uuid,session_uuid,environment_uuid,session_external_id,environment_external_id,model,worker_external_metadata)
 SELECT 'code_second',organization_uuid,workspace_uuid,uuid,environment_uuid,external_id,environment_external_id,'shared/model','{"model":"shared/model"}' FROM sessions;
 INSERT INTO message_batches (uuid,external_id,organization_uuid,workspace_uuid,created_by_api_key_uuid,api_variant,request_count,expires_at)
 VALUES ('aa000000-0000-4000-8000-000000000021','batch_second','aa000000-0000-4000-8000-000000000001','aa000000-0000-4000-8000-000000000003',gen_random_uuid(),'stable',2,now()+interval '1 day');
 INSERT INTO message_batch_requests (external_id,workspace_uuid,message_batch_uuid,request_index,custom_id,params,status) VALUES
 ('request_queued','aa000000-0000-4000-8000-000000000003','aa000000-0000-4000-8000-000000000021',0,'queued','{"model":"shared/model"}','queued'),
 ('request_completed','aa000000-0000-4000-8000-000000000003','aa000000-0000-4000-8000-000000000021',1,'completed','{"model":"shared/model"}','succeeded');
 `); err != nil {
		t.Fatal(err)
	}

	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var model string
	if err := database.QueryRowContext(ctx, `SELECT model->>'id' FROM agents WHERE external_id='agent_second'`).Scan(&model); err != nil || model != "shared-2/model" {
		t.Fatalf("migrated model=%q, %v", model, err)
	}
	if err := database.QueryRowContext(ctx, `SELECT model #>> '{}' FROM agents WHERE external_id='agent_first'`).Scan(&model); err != nil || model != "shared/model" {
		t.Fatalf("unchanged model=%q, %v", model, err)
	}
	for _, table := range []string{"sessions", "session_threads", "deployments", "deployment_runs"} {
		var snapshotModel, advisorModel string
		if err := database.QueryRowContext(ctx, fmt.Sprintf(`SELECT agent_snapshot->>'model',agent_snapshot#>>'{multiagent,advisor,model}' FROM %s WHERE external_id='session_second'`, table)).Scan(&snapshotModel, &advisorModel); err != nil || snapshotModel != "shared-2/model" || advisorModel != "shared-2/model" {
			t.Fatalf("%s snapshot=%q advisor=%q %v", table, snapshotModel, advisorModel, err)
		}
	}
	assertMigratedValues(t, database, `SELECT external_id,params->>'model' FROM message_batch_requests`, map[string]string{"request_queued": "shared-2/model", "request_completed": "shared/model"})
	assertMigratedValues(t, database, `SELECT external_id,worker_external_metadata->>'model' FROM code_sessions`, map[string]string{"code_second": "shared-2/model"})

	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	org := "aa000000-0000-4000-8000-000000000001"
	legacy, err := store.GetOrganizationLLMProvider(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := legacy
	duplicate.UUID = "aa000000-0000-4000-8000-000000000014"
	duplicate.ExternalID = "provider_duplicate"
	duplicate.Slug = "shared"
	if _, err := store.CreateLLMProvider(ctx, duplicate); !errors.Is(err, ErrLLMProviderSlugConflict) {
		t.Fatalf("cross-scope duplicate=%v", err)
	}
	duplicate.Slug = "second-organization"
	created, err := store.CreateLLMProvider(ctx, duplicate)
	if err != nil {
		t.Fatal(err)
	}
	organizations, err := store.ListLLMProviders(ctx, org, "")
	if err != nil || len(organizations) != 2 {
		t.Fatalf("organization list=%v %v", organizations, err)
	}
	if _, err := store.GetLLMProvider(ctx, org, "aa000000-0000-4000-8000-000000000002", created.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("workspace lookup leaked organization provider: %v", err)
	}
	if _, err := store.GetLLMProvider(ctx, "aa000000-0000-4000-8000-000000000099", "", created.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("organization lookup leaked tenant: %v", err)
	}
	created.Name = "Updated organization"
	if _, err := store.UpdateLLMProvider(ctx, created); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetLLMProvider(ctx, org, "", created.ExternalID)
	if err != nil || updated.Name != created.Name {
		t.Fatalf("organization update=%v %v", updated, err)
	}
	if _, err := migrations.Down(ctx); err == nil {
		t.Fatal("rollback must reject data that cannot fit the old singleton contract")
	}
	if err := store.DeleteLLMProvider(ctx, org, "", created.ExternalID); err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.Down(ctx); err != nil {
		t.Fatalf("rollback after removing new organization providers: %v", err)
	}
}

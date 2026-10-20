//go:build integration

package db

import (
	"context"
	"database/sql"
	"maps"
	"os"
	"testing"
)

func TestLLMProviderSlugMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置 TEST_MIGRATION_DATABASE_URL 运行真实迁移验收；跳过不能视为验收通过")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 89); err != nil {
		t.Fatal(err)
	}
	_, err := database.ExecContext(ctx, `
    INSERT INTO llm_providers (external_id,organization_uuid,workspace_uuid,name,base_url,api_key_last4,model_ids,ciphertext,nonce,wrapped_dek,format_version,key_provider,key_version,created_at) VALUES
    ('llmprov_first','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','First','https://first.example.com','0001','["kimi-k2.5","deepseek-ai/deepseek-v4.1-flash"]','x','x','x',1,'local',1,'2026-01-01'),
    ('llmprov_second','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','Second','https://second.example.com','0002','["glm-5","shared"]','x','x','x',1,'local',1,'2026-01-02'),
    ('llmprov_third','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','Third','https://third.example.com','0003','["shared"]','x','x','x',1,'local',1,'2026-01-03'),
    ('llmprov_other','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000003','Other','https://other.example.com','0004','["kimi-k2.5"]','x','x','x',1,'local',1,'2026-01-01');
    INSERT INTO agents (external_id,name,model,workspace_uuid,created_by_api_key_uuid) VALUES
    ('agent_object','object','{"id":"kimi-k2.5","speed":"fast"}','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000009'),
    ('agent_string','string','"deepseek-ai/deepseek-v4.1-flash"','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000009'),
    ('agent_shared','shared','{"id":"shared"}','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000009'),
    ('agent_missing','missing','{"id":"removed"}','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000009'),
    ('agent_other','other','{"id":"kimi-k2.5"}','bb000000-0000-4000-8000-000000000003','bb000000-0000-4000-8000-000000000009');
    INSERT INTO agent_versions (external_id,agent_external_id,agent_uuid,agent_created_at,agent_updated_at,workspace_uuid,version,model,name) VALUES
    ('agentver_object','agent_object','bb000000-0000-4000-8000-000000000010',now(),now(),'bb000000-0000-4000-8000-000000000002',1,'{"id":"glm-5"}','object');
    INSERT INTO sessions (external_id,agent_uuid,organization_uuid,workspace_uuid,environment_uuid,agent_version,agent_snapshot,environment_external_id,agent_external_id) VALUES
    ('sesn_object','bb000000-0000-4000-8000-000000000010','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000011',1,'{"id":"agent_object","model":{"id":"kimi-k2.5"}}','env_test','agent_object'),
    ('sesn_string','bb000000-0000-4000-8000-000000000010','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000011',1,'{"id":"agent_object","model":"glm-5"}','env_test','agent_object');
    INSERT INTO code_sessions (external_id,organization_uuid,workspace_uuid,session_uuid,environment_uuid,session_external_id,environment_external_id,model) VALUES
    ('cse_model','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000012','bb000000-0000-4000-8000-000000000011','sesn_object','env_test','kimi-k2.5'),
    ('cse_empty','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','bb000000-0000-4000-8000-000000000012','bb000000-0000-4000-8000-000000000011','sesn_object','env_test','');
   `)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 90); err != nil {
		t.Fatal(err)
	}

	assertMigratedValues(t, database, `SELECT external_id, slug FROM llm_providers`, map[string]string{
		"llmprov_first":  "default",
		"llmprov_second": "default-2",
		"llmprov_third":  "default-3",
		"llmprov_other":  "default",
	})
	assertMigratedValues(t, database, `SELECT external_id, model::text FROM agents`, map[string]string{
		"agent_object":  `{"id": "default/kimi-k2.5", "speed": "fast"}`,
		"agent_string":  `"default/deepseek-ai/deepseek-v4.1-flash"`,
		"agent_shared":  `{"id": "shared"}`,
		"agent_missing": `{"id": "removed"}`,
		"agent_other":   `{"id": "default/kimi-k2.5"}`,
	})
	assertMigratedValues(t, database, `SELECT external_id, model::text FROM agent_versions`, map[string]string{
		"agentver_object": `{"id": "default-2/glm-5"}`,
	})
	assertMigratedValues(t, database, `SELECT external_id, agent_snapshot->>'model' FROM sessions`, map[string]string{
		"sesn_object": `{"id": "default/kimi-k2.5"}`,
		"sesn_string": `default-2/glm-5`,
	})
	assertMigratedValues(t, database, `SELECT external_id, model FROM code_sessions`, map[string]string{
		"cse_model": "default/kimi-k2.5",
		"cse_empty": "",
	})
	if _, err := database.ExecContext(ctx, `UPDATE llm_providers SET slug = 'default' WHERE external_id = 'llmprov_second'`); err == nil {
		t.Fatal("允许了同一工作空间内重复的 slug")
	}
	if _, err := database.ExecContext(ctx, `UPDATE llm_providers SET slug = 'Bad/Slug' WHERE external_id = 'llmprov_second'`); err == nil {
		t.Fatal("允许了非法 slug")
	}
}

func assertMigratedValues(t *testing.T, database *sql.DB, query string, want map[string]string) {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make(map[string]string, len(want))
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		got[key] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, want) {
		t.Fatalf("%s = %#v, want %#v", query, got, want)
	}
}

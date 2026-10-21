//go:build integration

package db

import (
	"os"
	"testing"
)

func TestResumeAndBatchModelsMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置 TEST_MIGRATION_DATABASE_URL 运行真实迁移验收；跳过不能视为验收通过")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 90); err != nil {
		t.Fatal(err)
	}
	_, err := database.ExecContext(ctx, `
INSERT INTO llm_providers (external_id, organization_uuid, workspace_uuid, name, slug, base_url, api_key_last4, model_ids, ciphertext, nonce, wrapped_dek, format_version, key_provider, key_version) VALUES
('first', 'cc000000-0000-4000-8000-000000000001', 'cc000000-0000-4000-8000-000000000002', 'First', 'first', 'https://first.example.com', '0001', '["model","shared","vendor/model"]', 'x', 'x', 'x', 1, 'local', 1),
('second', 'cc000000-0000-4000-8000-000000000001', 'cc000000-0000-4000-8000-000000000002', 'Second', 'second', 'https://second.example.com', '0002', '["shared","first/model"]', 'x', 'x', 'x', 1, 'local', 1),
('other', 'cc000000-0000-4000-8000-000000000003', 'cc000000-0000-4000-8000-000000000004', 'Other', 'other', 'https://other.example.com', '0003', '["model"]', 'x', 'x', 'x', 1, 'local', 1);
INSERT INTO message_batches (uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid, api_variant, request_count, expires_at) VALUES
('cc000000-0000-4000-8000-000000000005', 'batch', 'cc000000-0000-4000-8000-000000000001', 'cc000000-0000-4000-8000-000000000002', gen_random_uuid(), 'stable', 20, now() + interval '1 day');
`)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, status, input, want string
	}{
		{"ambiguous", "queued", `{"model":"shared"}`, `{"model": "shared"}`},
		{"orphan", "queued", `{"model":"removed"}`, `{"model": "removed"}`},
		{"qualified", "queued", `{"model":"first/model"}`, `{"model": "first/model"}`},
		{"empty", "queued", `{"model":""}`, `{"model": ""}`},
		{"missing", "queued", `{}`, `{}`},
		{"null", "queued", `{"model":null}`, `{"model": null}`},
		{"legacy", "queued", `{"model":"model","pending_action":"approval"}`, `{"model": "first/model", "pending_action": "approval"}`},
		{"in_flight", "in_flight", `{"model":"vendor/model"}`, `{"model": "first/vendor/model"}`},
		{"completed", "succeeded", `{"model":"model"}`, `{"model": "model"}`},
	}
	workers := make(map[string]string, len(cases)+1)
	requests := make(map[string]string, len(cases))
	for index, test := range cases {
		_, err := database.ExecContext(ctx, `INSERT INTO code_sessions
(external_id, organization_uuid, workspace_uuid, session_uuid, environment_uuid, session_external_id, environment_external_id, model, worker_external_metadata)
VALUES ($1, 'cc000000-0000-4000-8000-000000000001', 'cc000000-0000-4000-8000-000000000002', gen_random_uuid(), gen_random_uuid(), 'session', 'environment', 'first/model', $2::jsonb)`, test.name, test.input)
		if err != nil {
			t.Fatal(err)
		}
		workers[test.name] = test.want
		if test.name == "completed" {
			workers[test.name] = `{"model": "first/model"}`
		}
		_, err = database.ExecContext(ctx, `INSERT INTO message_batch_requests
(external_id, workspace_uuid, message_batch_uuid, request_index, custom_id, params, status)
VALUES ($1, 'cc000000-0000-4000-8000-000000000002', 'cc000000-0000-4000-8000-000000000005', $2, $1, $3::jsonb, $4)`, test.name, index, test.input, test.status)
		if err != nil {
			t.Fatal(err)
		}
		requests[test.name] = test.want
	}
	_, err = database.ExecContext(ctx, `INSERT INTO code_sessions
(external_id, organization_uuid, workspace_uuid, session_uuid, environment_uuid, session_external_id, environment_external_id, worker_external_metadata)
VALUES ('other', 'cc000000-0000-4000-8000-000000000003', 'cc000000-0000-4000-8000-000000000004', gen_random_uuid(), gen_random_uuid(), 'other_session', 'other_environment', '{"model":"model"}')`)
	if err != nil {
		t.Fatal(err)
	}
	workers["other"] = `{"model": "other/model"}`
	if _, err := provider.UpTo(ctx, 91); err != nil {
		t.Fatal(err)
	}
	assertMigratedValues(t, database, `SELECT external_id, worker_external_metadata::text FROM code_sessions`, workers)
	assertMigratedValues(t, database, `SELECT external_id, params::text FROM message_batch_requests`, requests)
	if _, err := provider.DownTo(ctx, 90); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 91); err != nil {
		t.Fatal(err)
	}
	assertMigratedValues(t, database, `SELECT external_id, worker_external_metadata::text FROM code_sessions`, workers)
	assertMigratedValues(t, database, `SELECT external_id, params::text FROM message_batch_requests`, requests)
}

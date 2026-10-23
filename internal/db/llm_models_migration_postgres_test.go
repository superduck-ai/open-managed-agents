//go:build integration

package db

import (
	jsonv2 "encoding/json/v2"
	"os"
	"reflect"
	"testing"
)

func TestUnifiedLLMModelsMigrationPreservesModelIDsAndOrder(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 94); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
 INSERT INTO llm_providers (external_id,organization_uuid,workspace_uuid,name,slug,base_url,api_key_last4,model_ids,ciphertext,nonce,wrapped_dek,format_version,key_provider,key_version) VALUES
 ('llmprov_models','bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002','Workspace','workspace','https://models.example.com','0001','["second","first","default"]','x','x','x',1,'local',1),
 ('llmprov_org_models','bb000000-0000-4000-8000-000000000001',NULL,'Organization','organization','https://models.example.com','0002','[]','x','x','x',1,'local',1);
 `); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 95); err != nil {
		t.Fatal(err)
	}
	want := LLMModels{{ID: "second"}, {ID: "first"}, {ID: "default"}}
	assertModels := func() {
		t.Helper()
		var encoded []byte
		if err := database.QueryRowContext(ctx, `SELECT models FROM llm_providers WHERE external_id='llmprov_models'`).Scan(&encoded); err != nil {
			t.Fatal(err)
		}
		var got LLMModels
		if err := jsonv2.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("models: %+v want %+v", got, want)
		}
		var empty bool
		if err := database.QueryRowContext(ctx, `SELECT models='[]'::jsonb FROM llm_providers WHERE external_id='llmprov_org_models'`).Scan(&empty); err != nil || !empty {
			t.Fatalf("empty catalog: %t %v", empty, err)
		}
	}
	assertModels()
	for _, column := range []string{"model_ids", "model_context_windows", "model_options"} {
		assertMigrationColumnExists(t, ctx, database, "llm_providers", column, false)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	var restored bool
	if err := database.QueryRowContext(ctx, `SELECT model_ids='["second","first","default"]'::jsonb FROM llm_providers WHERE external_id='llmprov_models'`).Scan(&restored); err != nil || !restored {
		t.Fatalf("rollback: %t %v", restored, err)
	}
	if _, err := provider.UpTo(ctx, 95); err != nil {
		t.Fatal(err)
	}
	assertModels()
}

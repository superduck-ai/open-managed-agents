package db

import (
	"os"
	"testing"
)

func TestWebhookFailureWindowMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 69); err != nil {
		t.Fatal(err)
	}
	_, err := database.ExecContext(ctx, `INSERT INTO webhook_endpoints
 (external_id, organization_uuid, workspace_uuid, url, name, signing_secret, status, disabled_reason, consecutive_failures)
 VALUES ('wh_migration_enabled','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000002','https://example.com','enabled','test-secret','enabled',NULL,19),
 ('wh_migration_disabled','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000002','https://example.com','disabled','test-secret','disabled','old failure',20)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 70); err != nil {
		t.Fatal(err)
	}
	var count int
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM webhook_endpoints WHERE failure_started_at IS NULL AND (
 (external_id='wh_migration_enabled' AND status='enabled' AND disabled_reason IS NULL AND consecutive_failures=19) OR
 (external_id='wh_migration_disabled' AND status='disabled' AND disabled_reason='old failure' AND consecutive_failures=20))`).Scan(&count)
	if err != nil || count != 2 {
		t.Fatalf("historical state count=%d error=%v", count, err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 70); err != nil {
		t.Fatal(err)
	}
}

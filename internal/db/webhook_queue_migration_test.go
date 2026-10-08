package db

import (
	"database/sql"
	"os"
	"testing"
)

func TestWebhookQueueRetirementMigration(t *testing.T) {
	source := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if source == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, source)
	if _, err := provider.UpTo(ctx, 69); err != nil {
		t.Fatal(err)
	}
	_, err := database.ExecContext(ctx, `INSERT INTO jobs(external_id,workspace_uuid,type,status,attempts,payload,locked_by,locked_until)
 SELECT 'job_queue_'||kind||'_'||state,'00000000-0000-4000-8000-000000000002',kind,state,2,'{"event":{"id":"original"}}','old',NOW()+interval '1 hour'
 FROM unnest(ARRAY['webhook_delivery','object_cleanup']) kind CROSS JOIN unnest(ARRAY['pending','retry','running','completed','failed']) state`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 70); err != nil {
		t.Fatal(err)
	}
	assertWebhookFailureWindowColumn(t, database, true)
	var retired, untouched int
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND external_id IN ('job_queue_webhook_delivery_pending','job_queue_webhook_delivery_retry','job_queue_webhook_delivery_running') AND status='failed' AND locked_by IS NULL AND locked_until IS NULL AND attempts=2 AND payload->'event'->>'id'='original' AND payload->>'last_error' LIKE 'webhook delivery queue retired%'`).Scan(&retired)
	if err != nil || retired != 3 {
		t.Fatalf("retired=%d: %v", retired, err)
	}
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE locked_by='old' AND payload='{"event":{"id":"original"}}'::jsonb`).Scan(&untouched)
	if err != nil || untouched != 7 {
		t.Fatalf("untouched=%d: %v", untouched, err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	assertWebhookFailureWindowColumn(t, database, false)
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='failed'`).Scan(&retired)
	if err != nil || retired != 4 {
		t.Fatalf("rollback revived messages: %d %v", retired, err)
	}
}

func assertWebhookFailureWindowColumn(t *testing.T, database *sql.DB, want bool) {
	t.Helper()
	var exists bool
	err := database.QueryRowContext(t.Context(), `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='webhook_endpoints' AND column_name='failure_started_at'
		AND data_type='timestamp with time zone' AND is_nullable='YES'
	)`).Scan(&exists)
	if err != nil || exists != want {
		t.Fatalf("failure window column exists=%t want=%t: %v", exists, want, err)
	}
}

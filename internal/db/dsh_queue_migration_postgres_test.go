//go:build integration

package db

import (
	"os"
	"testing"
)

func TestDSHQueueMigrationPostgres(t *testing.T) {
	source := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if source == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, source)
	if _, err := provider.UpTo(ctx, 120); err != nil {
		t.Fatal(err)
	}
	_, err := database.ExecContext(ctx, `INSERT INTO jobs(external_id,workspace_uuid,type,status,payload,locked_by,locked_until,run_after)
        SELECT 'job_queue_'||state,'00000000-0000-4000-8000-000000000002','object_cleanup',state,'{}','old',NOW()+interval '1 hour',NOW()+interval '1 day'
        FROM unnest(ARRAY['running','completed']) state`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO dsh_input_executions(organization_uuid,workspace_uuid,code_session_uuid,input_id,session_thread_id,accepted_at)
        SELECT '00000000-0000-4000-8000-000000000001',workspace_uuid,'00000000-0000-4000-8000-000000000003',external_id,'thread',NOW() FROM jobs WHERE external_id LIKE 'job_queue_%'`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO dsh_input_deliveries(organization_uuid,workspace_uuid,code_session_uuid,input_execution_uuid,input_id,ack_subject,cleanup_job_uuid,status)
        SELECT e.organization_uuid,e.workspace_uuid,e.code_session_uuid,e.uuid,e.input_id,'old-ack',j.uuid,'waiting'
        FROM dsh_input_executions e JOIN jobs j ON j.external_id=e.input_id AND j.workspace_uuid=e.workspace_uuid`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 121); err != nil {
		t.Fatal(err)
	}
	var remaining, ready, completed int
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM dsh_input_executions WHERE status='accepted'`).Scan(&remaining)
	if err != nil || remaining != 2 {
		t.Fatalf("preserved executions=%d %v", remaining, err)
	}
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE external_id='job_queue_running' AND status='pending' AND locked_by IS NULL AND locked_until IS NULL AND run_after<=NOW()`).Scan(&ready)
	if err != nil || ready != 1 {
		t.Fatalf("cleanup ready=%d %v", ready, err)
	}
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE external_id='job_queue_completed' AND status='completed'`).Scan(&completed)
	if err != nil || completed != 1 {
		t.Fatalf("completed cleanup=%d %v", completed, err)
	}
	var removed bool
	err = database.QueryRowContext(ctx, `SELECT to_regclass(current_schema()||'.dsh_input_deliveries') IS NULL`).Scan(&removed)
	if err != nil || !removed {
		t.Fatalf("delivery table removed=%v %v", removed, err)
	}
}

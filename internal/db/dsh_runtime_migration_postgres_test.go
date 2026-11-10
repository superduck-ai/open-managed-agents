//go:build integration

package db

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestDSHRuntimeMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 114); err != nil {
		t.Fatal(err)
	}
	applied, err := migrations.Up(ctx)
	if err != nil || len(applied) != 1 {
		t.Fatalf("apply DSR migration: results=%v err=%v", applied, err)
	}
	tables := []string{"dsh_transcripts", "dsh_transcript_records", "dsh_worker_states", "dsh_input_executions", "dsh_permission_requests", "dsh_public_receipts"}
	for _, name := range tables {
		assertMigrationTableExists(t, ctx, database, name, true)
	}
	for _, name := range []string{"dsh_transcript_events", "dsh_input_deliveries", "dsh_control_receipts", "dsh_turns"} {
		assertMigrationTableExists(t, ctx, database, name, false)
	}
	var obsolete int
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND ((table_name='dsh_transcripts' AND column_name IN ('header','next_seq')) OR (table_name='dsh_worker_states' AND column_name IN ('model','trace_turns','model_control_sequence','model_control_input_id')))`).Scan(&obsolete)
	if err != nil || obsolete != 0 {
		t.Fatalf("obsolete columns=%d err=%v", obsolete, err)
	}
	scope := DSHScope{OrganizationUUID: "53000000-0000-0000-0000-000000000001", WorkspaceUUID: "53000000-0000-0000-0000-000000000002", CodeSessionUUID: "53000000-0000-0000-0000-000000000003"}
	workerInsert := `INSERT INTO dsh_worker_states(organization_uuid,workspace_uuid,code_session_uuid,context_snapshot) VALUES($1,$2,$3,$4)`
	if _, err := database.ExecContext(ctx, workerInsert, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID, `[]`); err == nil {
		t.Fatal("accepted non-object context snapshot")
	}
	digestInsert := `INSERT INTO dsh_public_receipts(organization_uuid,workspace_uuid,code_session_uuid,event_id,payload_digest) VALUES($1,$2,$3,$4,$5)`
	for _, invalid := range []string{"jcs-sha256-v1:" + strings.Repeat("a", 64), "unknown:" + strings.Repeat("a", 64), "exact-json-sha256-v1:short", "exact-json-sha256-v1:" + strings.Repeat("A", 64)} {
		if _, err := database.ExecContext(ctx, digestInsert, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID, "invalid", invalid); err == nil {
			t.Fatalf("accepted invalid digest %s", invalid)
		}
	}
	execClaimTest(t, ctx, database, digestInsert, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID, "exact", "exact-json-sha256-v1:"+strings.Repeat("a", 64))
	execClaimTest(t, ctx, database, workerInsert, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID, `{"model":"frozen"}`)
	var transcriptUUID string
	var offset int64
	err = database.QueryRowContext(ctx, `INSERT INTO dsh_transcripts(organization_uuid,workspace_uuid,code_session_uuid,dsh_session_id) VALUES($1,$2,$3,'root') RETURNING uuid,next_offset`, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID).Scan(&transcriptUUID, &offset)
	if err != nil || offset != 0 {
		t.Fatalf("new transcript offset=%d err=%v", offset, err)
	}
	payload := []byte(" {\"version\":4,\"integer\":9007199254740993}\n")
	recordInsert := `INSERT INTO dsh_transcript_records(organization_uuid,workspace_uuid,code_session_uuid,transcript_uuid,record_offset,payload) VALUES($1,$2,$3,$4,$5,$6)`
	for _, invalid := range []int64{-1, 9007199254740993} {
		if _, err := database.ExecContext(ctx, recordInsert, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID, transcriptUUID, invalid, payload); err == nil {
			t.Fatalf("accepted invalid offset %d", invalid)
		}
	}
	execClaimTest(t, ctx, database, recordInsert, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID, transcriptUUID, 0, payload)
	if _, err := database.ExecContext(ctx, recordInsert, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID, transcriptUUID, 0, payload); err == nil {
		t.Fatal("accepted duplicate record offset")
	}
	var stored []byte
	if err := database.QueryRowContext(ctx, `SELECT payload FROM dsh_transcript_records WHERE transcript_uuid=$1`, transcriptUUID).Scan(&stored); err != nil || !bytes.Equal(stored, payload) {
		t.Fatalf("byte identity changed: err=%v", err)
	}
	if _, err := migrations.DownTo(ctx, 114); err != nil {
		t.Fatal(err)
	}
	for _, name := range tables {
		assertMigrationTableExists(t, ctx, database, name, false)
	}
	assertMigrationTableExists(t, ctx, database, "sessions", true)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range tables {
		assertMigrationTableExists(t, ctx, database, name, true)
	}
}

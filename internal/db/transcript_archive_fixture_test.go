package db

import (
	"database/sql"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func newTranscriptArchiveTestDB(t *testing.T) (*DB, *sql.DB) {
	t.Helper()
	url := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, url)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	return &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}, database
}

func transcriptTestScope() TranscriptScope {
	id := uuid.NewV4().String()
	return TranscriptScope{OrganizationUUID: "52000000-0000-0000-0000-000000000001", WorkspaceUUID: "52000000-0000-0000-0000-000000000002", CodeSessionUUID: id, CodeSessionExternalID: "cse_" + id}
}

func seedTranscriptArchiveSession(t *testing.T, database *sql.DB, scope TranscriptScope) string {
	t.Helper()
	sessionUUID := uuid.NewV4().String()
	_, err := database.ExecContext(t.Context(), `INSERT INTO sessions
 (uuid,external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot)
 VALUES ($1,$2,$3,$4,$5,'env_archive_test',$6,'agent_archive_test',1,'{}')`, sessionUUID, "ses_"+sessionUUID, scope.OrganizationUUID, scope.WorkspaceUUID, uuid.NewV4().String(), uuid.NewV4().String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `INSERT INTO code_sessions
 (uuid,external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,environment_uuid,environment_external_id,worker_status,worker_lease_expires_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,'env_archive_test','idle',NULL)`, scope.CodeSessionUUID, scope.CodeSessionExternalID, scope.OrganizationUUID, scope.WorkspaceUUID, sessionUUID, "ses_"+sessionUUID, uuid.NewV4().String())
	if err != nil {
		t.Fatal(err)
	}
	return sessionUUID
}

func seedTranscriptArchiveEvent(t *testing.T, store *DB, scope TranscriptScope, sequence int64, agent *string, compact bool, created time.Time) CodeSessionInternalEvent {
	t.Helper()
	id := uuid.NewV4().String()
	row, err := NewCodeSessionInternalEventMapper(store.mapperDB).Insert(t.Context(), codeSessionInternalEventInsertParams{
		ExternalID: "ciev_" + id, OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, CodeSessionUUID: scope.CodeSessionUUID, CodeSessionExternalID: scope.CodeSessionExternalID, SequenceNum: sequence, AgentID: agent, IsCompaction: compact, EventType: "assistant", PayloadUUID: id, PayloadHash: "worker_hash", IdempotencyKey: id, Payload: []byte(`{"text":"历史"}`), EventMetadata: []byte(`{}`), CreatedAt: created,
	})
	if err != nil {
		t.Fatal(err)
	}
	return row.event()
}

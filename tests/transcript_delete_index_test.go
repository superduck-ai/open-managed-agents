package tests

import (
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestTranscriptDeletionIndexPlans(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("delete-index")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, make([]db.AppendCodeSessionInternalEventInput, 1))
	_, err := app.pool.Exec(t.Context(), `INSERT INTO code_session_internal_events
 (external_id, organization_uuid, workspace_uuid, code_session_uuid, code_session_external_id,
 sequence_num, event_type, payload_uuid, payload, payload_hash, idempotency_key, deleted_at)
 SELECT 'index_event_' || n, organization_uuid, workspace_uuid, code_session_uuid, code_session_external_id,
 n + 1, event_type, 'index_payload_' || n, payload, payload_hash, '',
 CASE WHEN n <= 100 THEN now()-interval '20 days' END
 FROM code_session_internal_events CROSS JOIN generate_series(1, 20000) n WHERE sequence_num=1`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(t.Context(), "ANALYZE code_session_internal_events"); err != nil {
		t.Fatal(err)
	}
	queries := []string{
		`SELECT DISTINCT organization_uuid, workspace_uuid, code_session_uuid, code_session_external_id FROM code_session_internal_events WHERE deleted_at < now()-interval '14 days' ORDER BY code_session_uuid LIMIT 100`,
		`SELECT EXISTS (SELECT 1 FROM code_session_internal_events e WHERE e.organization_uuid=$1 AND e.workspace_uuid=$2 AND e.code_session_uuid=$3 AND e.deleted_at < now()-interval '14 days' AND NOT EXISTS (SELECT 1 FROM transcript_archives a WHERE a.organization_uuid=e.organization_uuid AND a.workspace_uuid=e.workspace_uuid AND a.code_session_uuid=e.code_session_uuid AND a.state='attached' AND e.sequence_num BETWEEN a.from_sequence_num AND a.to_sequence_num))`,
	}
	for i, query := range queries {
		var args []any
		if i == 1 {
			args = []any{session.OrganizationUUID, session.WorkspaceUUID, session.UUID}
		}
		rows, err := app.pool.Query(t.Context(), "EXPLAIN (ANALYZE, BUFFERS) "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(line + "\n")
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		t.Log(plan.String())
		if !strings.Contains(plan.String(), "code_session_internal_events_deleted_session_idx") {
			t.Fatal("selective deleted-row query did not use the deletion index")
		}
	}
}

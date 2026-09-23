package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

func TestCodeSessionInputStatePostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
	// Only the columns queried here are needed for this isolated locking fixture.
	if _, err := database.ExecContext(ctx, `CREATE TABLE code_sessions (
 uuid uuid DEFAULT gen_random_uuid(), workspace_uuid uuid, session_uuid uuid,
 worker_status text, worker_external_metadata jsonb, created_at timestamptz DEFAULT now(), deleted_at timestamptz
 )`); err != nil {
		t.Fatal(err)
	}
	const workspace = "52000000-0000-0000-0000-000000000001"
	const session = "52000000-0000-0000-0000-000000000002"
	mapper := NewCodeSessionMapper(yourbatis.NewDB(database, yourbatis.DialectPostgres))
	if _, found, err := mapper.LockLatestInputState(ctx, workspace, session); err != nil || found {
		t.Fatalf("missing worker: found=%t err=%v", found, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO code_sessions(workspace_uuid,session_uuid,worker_status,worker_external_metadata) VALUES ($1,$2,'running','{}')`, workspace, session); err != nil {
		t.Fatal(err)
	}
	if _, found, err := mapper.LockLatestInputState(ctx, session, session); err != nil || found {
		t.Fatalf("foreign workspace: found=%t err=%v", found, err)
	}
	for _, tc := range []struct {
		name, metadata string
		pending        bool
	}{
		{"primary request", `{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider","session_thread_id":"sthr_primary"}}`, true},
		{"implicit primary", `{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider"}}`, true},
		{"legacy primary", `{"managed_agent_tool_permission_request":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider"}}`, true},
		{"child request", `{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider","session_thread_id":"sthr_child"}}`, false},
		{"legacy overridden by persisted null", `{"managed_agent_tool_permission_request":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider"},"managed_agent_tool_permission_request:tool":null}`, false},
		{"cleared request", `{"managed_agent_tool_permission_request:tool":null}`, false},
		{"unrelated metadata", `{"task_summary":"busy"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, `UPDATE code_sessions SET worker_external_metadata=$1::jsonb`, tc.metadata); err != nil {
				t.Fatal(err)
			}
			state, found, err := mapper.LockLatestInputState(ctx, workspace, session)
			pending, pendingErr := maevents.PendingToolEventIDs(state.WorkerExternalMetadata, "sthr_primary", "sthr_primary")
			if pendingErr != nil {
				t.Fatal(pendingErr)
			}
			if err != nil || !found || state.WorkerStatus != "running" || (len(pending) > 0) != tc.pending {
				t.Fatalf("input state=%+v found=%t err=%v", state, found, err)
			}
		})
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE code_sessions SET worker_status='requires_action',worker_external_metadata='{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider"}}'`); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if _, _, err := mapper.LockLatestInputState(blocked, workspace, session); err == nil || blocked.Err() == nil {
		t.Fatalf("input acceptance did not wait for worker update: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	state, found, err := mapper.LockLatestInputState(ctx, workspace, session)
	if err != nil || !found || state.WorkerStatus != "requires_action" || !strings.Contains(string(state.WorkerExternalMetadata), "managed_agent_tool_permission_request:tool") {
		t.Fatalf("committed input state=%+v found=%t err=%v", state, found, err)
	}
}

package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

type toolSettlementExecutor struct {
	calls   int
	bound   yourbatis.BoundSQL
	failure error
}

func (*toolSettlementExecutor) Dialect() yourbatis.Dialect { return yourbatis.DialectPostgres }
func (*toolSettlementExecutor) Query(context.Context, yourbatis.Statement, yourbatis.BoundSQL) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (e *toolSettlementExecutor) Exec(_ context.Context, statement yourbatis.Statement, bound yourbatis.BoundSQL) (sql.Result, error) {
	if statement.ID != "CodeSessionMapper.UpdateWorkerToolMetadata" {
		return nil, errors.New("unexpected mapper")
	}
	e.calls++
	e.bound = bound
	return driver.RowsAffected(1), e.failure
}
func TestToolResultSettlementPrecedesLaterIdleInSameLockedBatch(t *testing.T) {
	for _, tc := range []struct {
		name, eventType, reference, thread string
		other, failure                     bool
		wantCalls                          int
	}{
		{name: "cancelled pending approval", eventType: "agent.tool_result", reference: "tool", thread: "primary", wantCalls: 1},
		{name: "MCP completion", eventType: "agent.mcp_tool_result", reference: "tool", thread: "primary", wantCalls: 1},
		{name: "other thread remains pending", eventType: "agent.tool_result", reference: "tool", thread: "primary", other: true, wantCalls: 1},
		{name: "foreign thread result", eventType: "agent.tool_result", reference: "tool", thread: "child"},
		{name: "unknown result", eventType: "agent.tool_result", reference: "unknown", thread: "primary"},
		{name: "failed persistence retains local state", eventType: "agent.tool_result", reference: "tool", thread: "primary", failure: true, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(`{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"r","provider_tool_use_id":"provider","session_thread_id":"primary"}}`)
			if tc.other {
				raw = json.RawMessage(`{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"r","provider_tool_use_id":"provider","session_thread_id":"primary"},"managed_agent_tool_permission_request:other":{"public_event_id":"other","request_id":"r2","provider_tool_use_id":"provider2","session_thread_id":"child"}}`)
			}
			worker := codeSessionInputStateRow{ExternalID: "code", WorkerStatus: "requires_action", WorkerExternalMetadata: raw}
			executor := &toolSettlementExecutor{}
			if tc.failure {
				executor.failure = errors.New("write failed")
			}
			key := "tool_use_id"
			if tc.eventType == "agent.mcp_tool_result" {
				key = "mcp_tool_use_id"
			}
			payload, _ := json.Marshal(map[string]any{key: tc.reference, "is_error": true})
			event := SessionEvent{EventType: tc.eventType, ThreadExternalID: &tc.thread, Payload: payload}
			batch, err := sessionEventBatchTx(t.Context(), executor, Session{WorkspaceUUID: "workspace"}, SessionThread{ExternalID: "primary"}, &worker, event)
			if tc.failure {
				if err == nil || string(worker.WorkerExternalMetadata) != string(raw) {
					t.Fatal("failed settlement changed state")
				}
				return
			}
			if err != nil || len(batch) != 1 || executor.calls != tc.wantCalls {
				t.Fatalf("calls=%d want=%d batch=%d err=%v", executor.calls, tc.wantCalls, len(batch), err)
			}
			pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, "primary", "")
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"tool"}
			if tc.wantCalls == 1 {
				want = []string{}
			}
			if tc.other {
				want = []string{"other"}
			}
			if !reflect.DeepEqual(pending, want) {
				t.Fatalf("pending=%v want=%v", pending, want)
			}
			if tc.wantCalls == 1 {
				clear, _ := executor.bound.Args[1].Value.(bool)
				if clear == tc.other {
					t.Fatal("unrelated pending action details lost")
				}
				if reason := statusStopReason("idle", &sessionStopReason{Type: "end_turn"}, pending); (reason.Type == "end_turn") == tc.other {
					t.Fatalf("wrong final idle reason: %v", reason)
				}
			}
		})
	}
}

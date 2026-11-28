//go:build integration

package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestHarnessToolTaskCheckpointPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeID := seedClaimCodeSession(t, ctx, database, 1, sessioncontract.CloudHarnessRuntime, "running")
	sessionID := "sesn_claim_1"
	sessionUUID := "53000000-0000-0000-0001-000000000001"
	execClaimTest(t, ctx, database, `UPDATE sessions SET execution_runtime = $1 WHERE uuid = $2`, sessioncontract.CloudHarnessRuntime, sessionUUID)
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET status = 'active' WHERE external_id = $1`, codeID)
	execClaimTest(t, ctx, database, `INSERT INTO session_threads (external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id, agent_snapshot, status)
		VALUES ('sthr_harness', $1, $2, $3, $4, '{}', 'running')`, claimTestOrganization, claimTestWorkspace, sessionUUID, sessionID)
	ref := appendHarnessTestInput(t, store, sessionUUID, sessionID, "sevt_harness_input")
	until := time.Now().Add(time.Minute)

	initial, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{}, "initial-hash", until)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActiveHarnessTool(ctx, ref, "", initial.Lease.Epoch); !errors.Is(err, ErrNotFound) {
		t.Fatalf("model task had tool authority: %v", err)
	}
	thread := harness.ThreadID(ref.SessionUUID)
	user := harness.UserMessage{Text: "read"}
	if present, err := store.HarnessConversation(initial.Lease).HasMessageID(ctx, thread, ref.UUID); err != nil || present {
		t.Fatalf("user input was appended before the model returned: %t, %v", present, err)
	}
	if _, err := store.RecordHarnessModelEvents(ctx, initial.Lease, initial.CodeSession, HarnessModelStart, nil, nil); err != nil {
		t.Fatal(err)
	}
	spec := HarnessTool{CallID: "toolu_read", Name: "Read", Arguments: json.RawMessage(`{"file_path":"/tmp/example"}`)}
	requestedCall := spec.conversationCall()
	completion := &HarnessModelCompletion{Message: harness.AssistantMessage{Call: requestedCall}, Pending: harness.ToolWork{Call: requestedCall}, UserText: user.Text}
	if _, err := store.RecordHarnessModelEvents(ctx, initial.Lease, initial.CodeSession, HarnessModelEnd, nil, completion); err != nil {
		t.Fatal(err)
	}

	stale, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{}, "stale-hash", until)
	if err != nil || !stale.Superseded || stale.Task.Kind != HarnessToolTask {
		t.Fatalf("redelivery lost the pending tool task: %+v, %v", stale, err)
	}
	nextRef := appendHarnessTestInput(t, store, sessionUUID, sessionID, "sevt_harness_next")
	if _, err := store.ClaimHarnessTurn(ctx, nextRef, HarnessTurnDelivery{}, "out-of-order-hash", until); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("later input displaced the outstanding execution: %v", err)
	}
	toolDelivery := HarnessTurnDelivery{TaskUUID: stale.Task.UUID, ToolUUID: stale.Task.ToolUUID, Kind: HarnessToolTask}
	for _, mismatched := range []HarnessTurnDelivery{
		{TaskUUID: toolDelivery.TaskUUID, ToolUUID: toolDelivery.ToolUUID, Kind: HarnessModelTask},
		{TaskUUID: toolDelivery.TaskUUID, ToolUUID: uuid.NewString(), Kind: HarnessToolTask},
	} {
		if _, err := store.ClaimHarnessTurn(ctx, ref, mismatched, "invalid-hash", until); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("claimed task with mismatched delivery metadata: %v", err)
		}
	}
	call, err := store.ClaimHarnessTurn(ctx, ref, toolDelivery, "tool-hash", until)
	if err != nil || len(call.LegacyInput) != 0 {
		t.Fatalf("claim tool delivery = %+v, %v", call.Task, err)
	}
	if _, err := store.ActiveHarnessTool(ctx, ref, call.Task.UUID, initial.Lease.Epoch); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old owner received tool authority: %v", err)
	}
	tool, err := store.ActiveHarnessTool(ctx, ref, call.Task.UUID, call.Lease.Epoch)
	if err != nil || tool.CallID != spec.CallID || tool.Name != spec.Name || !json.Valid(tool.Arguments) || tool.Result != nil {
		t.Fatalf("tool : %+v, %v", tool, err)
	}
	result := sessioncontract.HarnessToolResult{Text: "file contents"}
	if _, err := store.FinishHarnessTool(ctx, sessionID, initial.Lease, tool.UUID, result, nil); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("old owner recorded a tool result: %v", err)
	}
	if _, err := store.FinishHarnessTool(ctx, sessionID, call.Lease, tool.UUID, result, nil); err != nil {
		t.Fatal(err)
	}

	staleTool, err := store.ClaimHarnessTurn(ctx, ref, toolDelivery, "stale-tool-hash", until)
	if err != nil || !staleTool.Superseded || staleTool.Task.Kind != HarnessModelTask {
		t.Fatalf("tool redelivery lost the model continuation: %+v, %v", staleTool, err)
	}
	modelDelivery := HarnessTurnDelivery{TaskUUID: staleTool.Task.UUID, ToolUUID: tool.UUID, Kind: HarnessModelTask}
	continuation, err := store.ClaimHarnessTurn(ctx, ref, modelDelivery, "next-model-hash", until)
	if err != nil || len(continuation.LegacyInput) != 0 {
		t.Fatalf("claim model delivery = %+v, %v", continuation.Task, err)
	}
	tools, err := store.ListHarnessTools(ctx, ref)
	if err != nil || len(tools) != 1 || tools[0].Result == nil || *tools[0].Result != result.Text {
		t.Fatalf("durable tool results = %+v, %v", tools, err)
	}
	toolInput := harness.ToolResult{CallID: harness.ToolCallID(spec.CallID), Text: result.Text}
	conversation := store.HarnessConversation(continuation.Lease)
	if found, err := conversation.HasMessageID(ctx, thread, tool.UUID+":result"); err != nil || found {
		t.Fatalf("tool result was appended before the model returned: %t, %v", found, err)
	}
	if _, err := store.RecordHarnessModelEvents(ctx, continuation.Lease, continuation.CodeSession, HarnessModelStart, nil, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	reply := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_harness_reply", EventType: "agent.message", CreatedAt: now, ProcessedAt: now,
		Payload: json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`)}
	if _, err := store.RecordHarnessModelEvents(ctx, continuation.Lease, continuation.CodeSession, HarnessModelEnd, []SessionEvent{reply}, &HarnessModelCompletion{Message: harness.AssistantMessage{Text: "done"}}); err != nil {
		t.Fatal(err)
	}
	next, err := store.ClaimHarnessTurn(ctx, nextRef, HarnessTurnDelivery{}, "next-turn-hash", until)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{}, "replayed-old-input", until); !errors.Is(err, ErrNotFound) {
		t.Fatalf("settled input displaced the next execution: %v", err)
	}
	history, err := store.HarnessConversation(next.Lease).LoadHistoryUpTo(ctx, thread, nil, nil)
	want := []harness.Message{
		user, harness.AssistantMessage{Call: spec.conversationCall()}, toolInput, harness.AssistantMessage{Text: "done"},
	}
	if err != nil || !slices.Equal(history, want) {
		t.Fatalf("recovered history = %+v, %v", history, err)
	}

	verifyHarnessFailedTurn(t, store, next, thread, want)
}

func TestHarnessRetryAfterToolRetainsCurrentTurnPostgres(t *testing.T) {
	ctx, _, store, initial := newHarnessControlTurn(t)
	ref := initial.Lease.Ref
	thread := harness.ThreadID(ref.SessionUUID)
	tool := HarnessTool{CallID: "toolu_read", Name: "Read", Arguments: json.RawMessage(`{}`)}
	completeHarnessControlModel(t, store, initial, tool)
	claimedTool := claimHarnessSubagentTask(t, store, ref, HarnessToolTask)
	result := sessioncontract.HarnessToolResult{Text: "output"}
	if _, err := store.FinishHarnessTool(ctx, harnessControlSessionID, claimedTool.Lease, claimedTool.Task.ToolUUID, result, nil); err != nil {
		t.Fatal(err)
	}
	model := claimHarnessSubagentTask(t, store, ref, HarnessModelTask)
	output := harness.ToolResult{CallID: harness.ToolCallID(tool.CallID), Text: result.Text}
	verifyHarnessFailedTurn(t, store, model, thread, []harness.Message{
		harness.UserMessage{Text: "Start"}, harness.AssistantMessage{Call: tool.conversationCall()}, output,
	})
}

func verifyHarnessFailedTurn(t *testing.T, store *DB, claim HarnessTurnClaim, thread harness.ThreadID, want []harness.Message) {
	t.Helper()
	ctx := t.Context()
	ref := claim.Lease.Ref
	delivery := HarnessTurnDelivery{TaskUUID: claim.Task.UUID, ToolUUID: claim.Task.ToolUUID, Kind: claim.Task.Kind}
	for attempt := 1; attempt <= HarnessModelAttempts; attempt++ {
		if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, nil, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelError, nil, nil); err != nil {
			t.Fatal(err)
		}
		if attempt < HarnessModelAttempts {
			retry, err := store.ClaimHarnessTurn(ctx, ref, delivery, "retry-hash", time.Now().Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			claim = retry
		}
	}
	state, err := store.HarnessTurnState(ctx, ref)
	if err != nil || state != "failed" {
		t.Fatalf("retry exhaustion = %q, %v", state, err)
	}
	conversation := store.HarnessConversation(claim.Lease)
	history, err := conversation.LoadHistoryUpTo(ctx, thread, nil, nil)
	if err != nil || !slices.Equal(history, want) {
		t.Fatalf("failed turn changed prior history: %+v, %v", history, err)
	}
}
func seedHarnessToolInput(t *testing.T, database *sql.DB, sessionUUID, sessionID, eventID string) HarnessTurnRef {
	t.Helper()
	ref := HarnessTurnRef{UUID: uuid.NewString(), OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, SessionUUID: sessionUUID}
	execClaimTest(t, t.Context(), database, `INSERT INTO session_events (uuid, external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id, event_type, payload)
		VALUES ($1, $2, $3, $4, $5, $6, 'user.message', '{"content":[{"type":"text","text":"read"}]}')`,
		ref.UUID, eventID, ref.OrganizationUUID, ref.WorkspaceUUID, ref.SessionUUID, sessionID)
	return ref
}

func appendHarnessTestInput(t *testing.T, store *DB, sessionUUID, sessionID, eventID string) HarnessTurnRef {
	t.Helper()
	ref := HarnessTurnRef{UUID: uuid.NewString(), OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, SessionUUID: sessionUUID}
	if _, err := store.AppendSessionUserEvents(t.Context(), ref.WorkspaceUUID, sessionID, []SessionEvent{{
		UUID: ref.UUID, ExternalID: eventID, EventType: "user.message", CreatedAt: time.Now().UTC(),
		Payload: json.RawMessage(`{"content":[{"type":"text","text":"read"}]}`),
	}}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	return ref
}

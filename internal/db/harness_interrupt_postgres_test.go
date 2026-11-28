//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestHarnessInterruptFirstQueuedInputPostgres(t *testing.T) {
	ctx, _, store, first := newHarnessControlInput(t)
	second := appendHarnessTestInput(t, store, first.SessionUUID, harnessControlSessionID, "sevt_harness_queued_second")
	until := time.Now().Add(time.Minute)
	if _, err := store.ClaimHarnessTurn(ctx, second, HarnessTurnDelivery{}, "second-hash", until); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("later input bypassed the queue: %v", err)
	}
	interrupt := newHarnessInterruptTestEvent(nil)
	changes, err := store.AppendSessionUserEvents(ctx, first.WorkspaceUUID, harnessControlSessionID, []SessionEvent{interrupt}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireInterruptEndTurn(t, changes.Events, "session.status_idle", "")
	requireInterruptEndTurn(t, changes.Events, "session.thread_status_idle", "sthr_harness")
	if input, found, err := store.FindHarnessInput(ctx, first); err != nil || !found || input.State != harnessInputCancelled {
		t.Fatalf("first queued input was not cancelled: %q, %t, %v", input.State, found, err)
	}
	if _, err := store.HarnessTurnState(ctx, first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("queued cancellation created a turn: %v", err)
	}
	if _, err := store.ClaimHarnessTurn(ctx, second, HarnessTurnDelivery{}, "second-hash", until); err != nil {
		t.Fatalf("second input could not run after interrupt: %v", err)
	}
	if _, err := store.ClaimHarnessTurn(ctx, first, HarnessTurnDelivery{}, "stale-hash", until); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale delivery was not recognized after the next claim: %v", err)
	}
}

func TestHarnessInterruptSubagentResumesParentPostgres(t *testing.T) {
	ctx, _, store, parent := newHarnessControlTurn(t)
	ref := parent.Lease.Ref
	child := startInterruptTestSubagent(t, store, parent)
	if child.Subagent == nil {
		t.Fatal("subagent checkpoint has no thread")
	}
	startID := maevents.HarnessModelRequestID(child.CodeSession.ExternalID, ref.UUID, child.Lease.Epoch)
	now := time.Now().UTC()
	start := SessionEvent{UUID: uuid.NewString(), ExternalID: startID, EventType: "span.model_request_start",
		CreatedAt: now, ProcessedAt: now, ThreadExternalID: &child.Subagent.ExternalID, Payload: json.RawMessage(`{"model":"model"}`)}
	if _, err := store.RecordHarnessModelEvents(ctx, child.Lease, child.CodeSession, HarnessModelStart, []SessionEvent{start}, nil); err != nil {
		t.Fatal(err)
	}
	event := newHarnessInterruptTestEvent(&child.Subagent.ExternalID)
	changes, err := store.AppendSessionUserEvents(ctx, ref.WorkspaceUUID, harnessControlSessionID, []SessionEvent{event}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireInterruptEndTurn(t, changes.Events, "session.thread_status_idle", child.Subagent.ExternalID)
	if !slices.ContainsFunc(changes.Events, func(event SessionEvent) bool {
		var result struct {
			IsError bool `json:"is_error"`
		}
		return event.EventType == "agent.tool_result" && json.Unmarshal(event.Payload, &result) == nil && result.IsError
	}) {
		t.Fatal("missing error tool result for interrupted delegation")
	}
	if !slices.Contains(changes.SettledInputEventIDs, event.ExternalID) || !slices.ContainsFunc(changes.Events, func(event SessionEvent) bool {
		return event.ExternalID == startID+"_end" && event.ThreadExternalID != nil && *event.ThreadExternalID == child.Subagent.ExternalID
	}) {
		t.Fatalf("interruption did not settle the child request: %+v", changes)
	}
	if err := store.RenewHarnessTurn(ctx, child.Lease, time.Now().Add(time.Minute)); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("old child worker could renew after stop: %v", err)
	}
	if _, err := store.RecordHarnessModelEvents(ctx, child.Lease, child.CodeSession, HarnessModelEnd, nil, &HarnessModelCompletion{Message: harness.AssistantMessage{Text: "late"}}); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("old child worker could commit after stop: %v", err)
	}
	recovered, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{
		TaskUUID: child.Task.UUID, ToolUUID: child.Task.ToolUUID, Kind: child.Task.Kind,
	}, "stale-delivery", time.Now().Add(time.Minute))
	if err != nil || !recovered.Superseded || recovered.Task.Kind != HarnessModelTask || recovered.Task.ToolUUID != child.Task.ToolUUID {
		t.Fatalf("replayed child delivery cannot hand off to the parent: %+v, %v", recovered, err)
	}
	if _, err := store.AppendSessionUserEvents(ctx, ref.WorkspaceUUID, harnessControlSessionID, []SessionEvent{newHarnessInterruptTestEvent(&child.Subagent.ExternalID)}, nil, nil, nil); err != nil {
		t.Fatalf("repeated child stop should be a no-op: %v", err)
	}
	next := claimHarnessSubagentTask(t, store, ref, HarnessModelTask)
	if next.Subagent != nil || next.Task.UUID != recovered.Task.UUID {
		t.Fatalf("child stop changed the parent continuation: %+v", next)
	}
	tool, found, err := NewCloudHarnessMapper(store.mapperDB).FindTool(ctx, ref, next.Task.ToolUUID)
	if err != nil || !found || !tool.Failed || tool.Result == nil {
		t.Fatalf("parent has no cancelled tool outcome: %+v, %t, %v", tool, found, err)
	}
}

func TestHarnessSessionStopCancelsChildSandboxToolPostgres(t *testing.T) {
	ctx, _, store, parent := newHarnessControlTurn(t)
	ref := parent.Lease.Ref
	child := startInterruptTestSubagent(t, store, parent)
	if child.Subagent == nil {
		t.Fatal("subagent checkpoint has no thread")
	}
	completeHarnessControlModel(t, store, child, HarnessTool{CallID: "child-read", Name: "Read", Arguments: json.RawMessage(`{}`)})
	activeTool := claimHarnessSubagentTask(t, store, ref, HarnessToolTask)
	scope := SandboxToolCallScope{
		OrganizationUUID: ref.OrganizationUUID, WorkspaceUUID: ref.WorkspaceUUID, CodeSessionID: activeTool.CodeSession.ExternalID,
		ThreadID: child.Subagent.ExternalID, CallID: activeTool.Task.ToolUUID, WorkerEpoch: activeTool.Lease.Epoch,
	}
	if _, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{
		Scope: scope, Tool: "read", Input: []byte(`{}`), QueueTimeout: time.Minute, Timeout: time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ParkHarnessTool(ctx, activeTool.Lease, activeTool.Task.ToolUUID); err != nil {
		t.Fatal(err)
	}
	event := newHarnessInterruptTestEvent(nil)
	changes, err := store.AppendSessionUserEvents(ctx, ref.WorkspaceUUID, harnessControlSessionID, []SessionEvent{event}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireInterruptEndTurn(t, changes.Events, "session.status_idle", "")
	requireInterruptEndTurn(t, changes.Events, "session.thread_status_idle", "sthr_harness")
	state, err := store.HarnessTurnState(ctx, ref)
	if err != nil || state != HarnessInterrupted {
		t.Fatalf("root stop should settle the whole turn: %q, %v", state, err)
	}
	sandboxTool, err := store.GetSandboxToolCall(ctx, scope)
	if err != nil || sandboxTool.State != "cancelled" {
		t.Fatalf("queued sandbox call was left runnable after stop: %+v, %v", sandboxTool, err)
	}
	if due, err := store.ListReadyHarnessInputs(ctx, 64); err != nil || len(due) != 0 {
		t.Fatalf("stopped session still has a ready result input: %+v, %v", due, err)
	}
	input, err := NewSessionEventMapper(store.mapperDB).FindByUUID(ctx, ref.WorkspaceUUID, ref.SessionUUID, ref.UUID)
	if err != nil || input.ProcessedAt == nil {
		t.Fatalf("interrupted input still blocks later turns: %+v, %v", input, err)
	}
	history, err := store.HarnessConversation(parent.Lease).LoadHistoryUpTo(ctx, harness.ThreadID(ref.SessionUUID), nil, nil)
	if err != nil || len(history) != 3 {
		t.Fatalf("root history must settle the delegated tool call: %+v, %v", history, err)
	}
	if result, ok := history[2].(harness.ToolResult); !ok || !result.Failed {
		t.Fatalf("root conversation has a dangling tool call: %+v", history)
	}
}

func requireInterruptEndTurn(t *testing.T, events []SessionEvent, eventType, threadID string) {
	t.Helper()
	for _, event := range events {
		if event.EventType != eventType {
			continue
		}
		var status sessionStatusPayload
		if err := json.Unmarshal(event.Payload, &status); err != nil {
			t.Fatal(err)
		}
		if status.SessionThreadID != threadID {
			continue
		}
		if status.StopReason == nil || status.StopReason.Type != "end_turn" {
			t.Fatalf("%s for %q: expected end_turn, got %+v", eventType, threadID, status.StopReason)
		}
		return
	}
	t.Fatalf("missing %s for %q", eventType, threadID)
}

func startInterruptTestSubagent(t *testing.T, store *DB, parent HarnessTurnClaim) HarnessTurnClaim {
	t.Helper()
	ctx := t.Context()
	ref := parent.Lease.Ref
	completeHarnessControlModel(t, store, parent, HarnessTool{CallID: "delegate", Name: "subagent", Arguments: json.RawMessage(`{}`)})
	delegated := claimHarnessSubagentTask(t, store, ref, HarnessToolTask)
	tool, err := store.ActiveHarnessTool(ctx, ref, delegated.Task.UUID, delegated.Lease.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartHarnessSubagent(ctx, delegated.Lease, delegated.CodeSession, tool, HarnessDelegation{
		AgentSnapshot: json.RawMessage(`{"name":"Helper","type":"inline"}`), Prompt: "Help the parent.",
	}); err != nil {
		t.Fatal(err)
	}
	return claimHarnessSubagentTask(t, store, ref, HarnessSubagentTask)
}

func newHarnessInterruptTestEvent(threadID *string) SessionEvent {
	now := time.Now().UTC()
	return SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_" + uuid.NewString(), CreatedAt: now, ProcessedAt: now,
		ThreadExternalID: threadID, EventType: "user.interrupt", Payload: json.RawMessage(`{"type":"user.interrupt"}`)}
}

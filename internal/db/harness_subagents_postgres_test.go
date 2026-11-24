//go:build integration

package db

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestHarnessSubagentDelegatesAndResumesPostgres(t *testing.T) {
	ctx, _, store, parent := newHarnessControlTurn(t)
	ref := parent.Lease.Ref
	completeHarnessControlModel(t, store, parent, HarnessTool{
		CallID: "toolu_delegate", Name: "subagent", Arguments: json.RawMessage(`{"description":"Research","prompt":"Inspect the project"}`),
	})
	toolClaim := claimHarnessSubagentTask(t, store, ref, HarnessToolTask)
	tool, err := store.ActiveHarnessTool(ctx, ref, toolClaim.Task.UUID, toolClaim.Lease.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	target := HarnessDelegation{ThreadID: "sthr_harness", Prompt: "Cannot delegate to the primary thread"}
	if _, err := store.StartHarnessSubagent(ctx, toolClaim.Lease, toolClaim.CodeSession, tool, target); !errors.Is(err, ErrSessionInputConflict) {
		t.Fatalf("accepted the primary thread as a child: %v", err)
	}
	if _, err := store.StartHarnessSubagent(ctx, parent.Lease, toolClaim.CodeSession, tool, HarnessDelegation{Prompt: "Stale"}); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("accepted a stale lease: %v", err)
	}
	delegation := HarnessDelegation{
		AgentSnapshot: json.RawMessage(`{"type":"inline","name":"Research"}`), Prompt: "Inspect the project",
	}
	events, err := store.StartHarnessSubagent(ctx, toolClaim.Lease, toolClaim.CodeSession, tool, delegation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartHarnessSubagent(ctx, toolClaim.Lease, toolClaim.CodeSession, tool, delegation); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("replayed activation created another thread: %v", err)
	}
	child := claimHarnessSubagentTask(t, store, ref, HarnessSubagentTask)
	if child.Subagent == nil || child.Subagent.Status != "running" || child.Subagent.ParentThreadUUID == nil || child.InputEventUUID != tool.UUID {
		t.Fatalf("missing scoped child thread: %+v", child.Subagent)
	}
	assertHarnessCoordinationEvents(t, events, "agent.thread_message_sent", "agent.thread_message_received", child.Subagent.ExternalID)
	thread := harness.ThreadID(child.Subagent.UUID)
	conversation := store.HarnessConversation(child.Lease)
	history, _, prefix, err := conversation.LoadHistoryWithAncestors(ctx, thread)
	if err != nil || prefix != 0 || len(history) != 0 {
		t.Fatalf("child loaded parent context: %+v, %d, %v", history, prefix, err)
	}
	if prompt, err := store.HarnessDelegationPrompt(ctx, child.Lease, child.CodeSession.SessionExternalID); err != nil || prompt != delegation.Prompt {
		t.Fatalf("missing staged child input: %q, %v", prompt, err)
	}
	childInputTool := HarnessTool{CallID: "toolu_child_read", Name: "Read", Arguments: json.RawMessage(`{"file_path":"/tmp/example"}`)}
	completeHarnessControlModel(t, store, child, childInputTool)
	childTool := claimHarnessSubagentTask(t, store, ref, HarnessToolTask)
	if childTool.Subagent == nil || childTool.Subagent.UUID != child.Subagent.UUID {
		t.Fatalf("child lost its thread on tool handoff: %+v", childTool.Subagent)
	}
	outcome := sessioncontract.HarnessToolResult{Text: "project files"}
	if _, err := store.FinishHarnessTool(ctx, harnessControlSessionID, childTool.Lease, childTool.Task.UUID, outcome, nil); err != nil {
		t.Fatal(err)
	}
	resumed := claimHarnessSubagentTask(t, store, ref, HarnessModelTask)
	if resumed.Subagent == nil || resumed.Subagent.UUID != child.Subagent.UUID {
		t.Fatalf("child lost its thread after tool: %+v", resumed.Subagent)
	}
	if count, err := store.HarnessExecutionToolCount(ctx, resumed.Lease); err != nil || count != 1 {
		t.Fatalf("child tool budget counted the parent call: %d, %v", count, err)
	}
	reports := finishHarnessSubagentModel(t, store, resumed, "Found a README.")
	assertHarnessCoordinationEvents(t, reports, "agent.thread_message_received", "agent.thread_message_sent", child.Subagent.ExternalID)
	parentResume := claimHarnessSubagentTask(t, store, ref, HarnessModelTask)
	if parentResume.Subagent != nil || parentResume.Task.ToolUUID != tool.UUID {
		t.Fatalf("parent did not resume its own tool call: %+v", parentResume)
	}
	if count, err := store.HarnessExecutionToolCount(ctx, parentResume.Lease); err != nil || count != 1 {
		t.Fatalf("parent tool budget counted the child call: %d, %v", count, err)
	}
	row, found, err := NewCloudHarnessMapper(store.mapperDB).FindTool(ctx, ref, tool.UUID)
	if err != nil || !found || row.Result == nil || !strings.Contains(*row.Result, "Found a README.") || row.Failed {
		t.Fatalf("parent tool result: %+v, %v", row, err)
	}
	verifyHarnessFollowupFailure(t, ctx, store, parentResume, *child.Subagent, thread)
}

func verifyHarnessFollowupFailure(t *testing.T, ctx context.Context, store *DB, parent HarnessTurnClaim, child SessionThread, thread harness.ThreadID) {
	t.Helper()
	ref := parent.Lease.Ref
	completeHarnessControlModel(t, store, parent, HarnessTool{CallID: "toolu_followup", Name: "send_message", Arguments: json.RawMessage(`{}`)})
	followup := claimHarnessSubagentTask(t, store, ref, HarnessToolTask)
	tool, err := store.ActiveHarnessTool(ctx, ref, followup.Task.UUID, followup.Lease.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.StartHarnessSubagent(ctx, followup.Lease, followup.CodeSession, tool, HarnessDelegation{ThreadID: child.ExternalID, Prompt: "Please verify the license."})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(events, func(event SessionEvent) bool { return event.EventType == "session.thread_created" }) {
		t.Fatal("follow-up created another thread")
	}
	for range HarnessModelAttempts {
		next := claimHarnessSubagentTask(t, store, ref, HarnessSubagentTask)
		if next.Subagent == nil || next.Subagent.UUID != child.UUID {
			t.Fatalf("follow-up changed threads: %+v", next.Subagent)
		}
		if _, err := store.RecordHarnessModelEvents(ctx, next.Lease, next.CodeSession, HarnessModelStart, nil, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := store.RecordHarnessModelEvents(ctx, next.Lease, next.CodeSession, HarnessModelError, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	next := claimHarnessSubagentTask(t, store, ref, HarnessModelTask)
	if next.Subagent != nil || next.Task.ToolUUID != tool.UUID {
		t.Fatalf("child failure did not resume parent: %+v", next)
	}
	row, found, err := NewCloudHarnessMapper(store.mapperDB).FindTool(ctx, ref, tool.UUID)
	if err != nil || !found || row.Result == nil || !row.Failed {
		t.Fatalf("missing parent error result: %+v, %v", row, err)
	}
	history, err := store.HarnessConversation(next.Lease).LoadHistoryUpTo(ctx, thread, nil, nil)
	if err != nil || len(history) != 4 || history[len(history)-1] != (harness.AssistantMessage{Text: "Found a README."}) {
		t.Fatalf("failed follow-up changed the existing child history: %+v, %v", history, err)
	}
}

func claimHarnessSubagentTask(t *testing.T, store *DB, ref HarnessTurnRef, kind string) HarnessTurnClaim {
	t.Helper()
	ctx := t.Context()
	task, _, err := store.HarnessTurnTask(ctx, ref)
	if err != nil || task.Kind != kind {
		t.Fatalf("current task = %+v, want %s: %v", task, kind, err)
	}
	claim, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{TaskUUID: task.UUID, ToolUUID: task.ToolUUID, Kind: task.Kind}, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || claim.State != "running" {
		t.Fatalf("claim task %s: %+v, %v", task.UUID, claim, err)
	}
	return claim
}

func finishHarnessSubagentModel(t *testing.T, store *DB, claim HarnessTurnClaim, text string) []SessionEvent {
	t.Helper()
	ctx := t.Context()
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, nil, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	payload, err := json.Marshal(struct {
		Content []harnessThreadText `json:"content"`
	}{[]harnessThreadText{{Type: "text", Text: text}}})
	if err != nil {
		t.Fatal(err)
	}
	events := []SessionEvent{{
		UUID: uuid.NewString(), ExternalID: "sevt_" + uuid.NewString(), EventType: "agent.message",
		CreatedAt: now, ProcessedAt: now, Payload: payload, ThreadExternalID: &claim.Subagent.ExternalID,
	}}
	stored, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelEnd, events, &HarnessModelCompletion{Message: harness.AssistantMessage{Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func assertHarnessCoordinationEvents(t *testing.T, events []SessionEvent, parentKind, childKind, childID string) {
	t.Helper()
	var parent, child bool
	for _, event := range events {
		if event.ThreadExternalID == nil {
			continue
		}
		parent = parent || event.EventType == parentKind && *event.ThreadExternalID == "sthr_harness"
		child = child || event.EventType == childKind && *event.ThreadExternalID == childID
	}
	if !parent || !child {
		t.Fatalf("missing coordination messages on the parent or child thread: %+v", events)
	}
}

func TestHarnessSubagentInheritsOnlySettledParentHistoryPostgres(t *testing.T) {
	ctx, _, store, parent := newHarnessControlTurn(t)
	ref := parent.Lease.Ref
	completeHarnessControlModel(t, store, parent, HarnessTool{CallID: "toolu_delegate", Name: "subagent", Arguments: json.RawMessage(`{}`)})
	call := claimHarnessSubagentTask(t, store, ref, HarnessToolTask)
	tool, err := store.ActiveHarnessTool(ctx, ref, call.Task.UUID, call.Lease.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.StartHarnessSubagent(ctx, call.Lease, call.CodeSession, tool, HarnessDelegation{
		AgentSnapshot: json.RawMessage(`{"type":"inline","name":"Research"}`),
		Prompt:        "Additional context", InheritContext: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	child := claimHarnessSubagentTask(t, store, ref, HarnessSubagentTask)
	if child.Subagent == nil {
		t.Fatal("child execution thread is missing")
	}
	history, _, prefix, err := store.HarnessConversation(child.Lease).LoadHistoryWithAncestors(ctx, harness.ThreadID(child.Subagent.UUID))
	want := []harness.Message{harness.UserMessage{Text: "Start"}}
	if err != nil || prefix != 1 || !slices.Equal(history, want) {
		t.Fatalf("history must include only parent history before the outstanding call: %+v, %d, %v", history, prefix, err)
	}
}

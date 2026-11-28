//go:build integration

package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const harnessControlSessionID = "sesn_claim_1"

func newHarnessControlTurn(t *testing.T) (context.Context, *sql.DB, *DB, HarnessTurnClaim) {
	t.Helper()
	ctx, database, store, ref := newHarnessControlInput(t)
	claim, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{}, "initial-hash", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return ctx, database, store, claim
}

func newHarnessControlInput(t *testing.T) (context.Context, *sql.DB, *DB, HarnessTurnRef) {
	t.Helper()
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeID := seedClaimCodeSession(t, ctx, database, 1, sessioncontract.CloudHarnessRuntime, "running")
	sessionUUID := "53000000-0000-0000-0001-000000000001"
	execClaimTest(t, ctx, database, `UPDATE sessions SET execution_runtime = $1 WHERE uuid = $2`, sessioncontract.CloudHarnessRuntime, sessionUUID)
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET status = 'active' WHERE external_id = $1`, codeID)
	execClaimTest(t, ctx, database, `INSERT INTO session_threads (external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id, agent_snapshot, status)
  VALUES ('sthr_harness', $1, $2, $3, $4, '{}', 'running')`, claimTestOrganization, claimTestWorkspace, sessionUUID, harnessControlSessionID)
	ref := appendHarnessTestInput(t, store, sessionUUID, harnessControlSessionID, "sevt_harness_input")
	return ctx, database, store, ref
}

func completeHarnessControlModel(t *testing.T, store *DB, claim HarnessTurnClaim, tool HarnessTool, events ...SessionEvent) []SessionEvent {
	t.Helper()
	ctx := t.Context()
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, nil, nil); err != nil {
		t.Fatal(err)
	}
	call := tool.conversationCall()
	var pending harness.Work = harness.ToolWork{Call: call}
	if tool.Name == sessioncontract.HarnessAskUserQuestion {
		pending = harness.UserChoiceWork{Call: call}
	}
	completion := &HarnessModelCompletion{Message: harness.AssistantMessage{Call: call}, Pending: pending}
	if claim.Task.Kind == "" {
		completion.UserText = "Start"
	}
	recorded, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelEnd, events, completion)
	if err != nil {
		t.Fatal(err)
	}
	return recorded
}

func TestHarnessChoiceWaitAndResumePostgres(t *testing.T) {
	ctx, _, store, claim := newHarnessControlTurn(t)
	ref := claim.Lease.Ref
	state := store.HarnessState(HarnessTurnLease{Ref: ref})
	thread := harness.ThreadID(ref.SessionUUID)
	now := time.Now().UTC()
	asked := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_asked", EventType: "agent.custom_tool_use", CreatedAt: now, ProcessedAt: now,
		Payload: json.RawMessage(`{"name":"AskUserQuestion","input":{}}`)}
	emitted := completeHarnessControlModel(t, store, claim, HarnessTool{
		CallID: "toolu_pick", Name: sessioncontract.HarnessAskUserQuestion,
		Arguments: json.RawMessage(`{"questions":[{"id":"color","question":"Which color?","options":[{"label":"Blue"},{"label":"Green"}]}]}`),
	}, asked)
	if !slices.ContainsFunc(emitted, func(event SessionEvent) bool {
		var status sessionStatusPayload
		return event.EventType == "session.status_idle" && json.Unmarshal(event.Payload, &status) == nil &&
			status.StopReason != nil && status.StopReason.Type == "requires_action" && slices.Contains(status.StopReason.EventIDs, asked.ExternalID)
	}) {
		t.Fatalf("missing requires_action status: %+v", emitted)
	}
	duplicate, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{}, "duplicate-hash", time.Now().Add(time.Minute))
	if err != nil || duplicate.State != HarnessWaitingChoice || duplicate.Superseded || duplicate.Lease.Epoch != 0 {
		t.Fatalf("parked delivery was reclaimed: %+v, %v", duplicate, err)
	}
	choices, err := state.GetPendingUserChoices(ctx, thread)
	if err != nil || len(choices) != 1 || string(choices[0].ID) != asked.ExternalID {
		t.Fatalf("pending choices: %+v, %v", choices, err)
	}

	answer := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_answer", EventType: "user.custom_tool_result", CreatedAt: now.Add(time.Second),
		ProcessedAt: now.Add(time.Second), ToolUseID: &asked.ExternalID,
		Payload: json.RawMessage(`{"custom_tool_use_id":"sevt_asked","content":[{"type":"text","text":"Red"}]}`)}
	if _, err := store.AppendSessionUserEvents(ctx, ref.WorkspaceUUID, harnessControlSessionID, []SessionEvent{answer}, nil, nil, nil); !errors.Is(err, ErrSessionInputConflict) {
		t.Fatalf("invalid option: %v", err)
	}
	answer.Payload = json.RawMessage(`{"custom_tool_use_id":"sevt_asked","content":[{"type":"text","text":"{\"answers\":[{\"id\":\"color\",\"selected\":[\"Blue\"]}]}"}]}`)
	changes, err := store.AppendSessionUserEvents(ctx, ref.WorkspaceUUID, harnessControlSessionID, []SessionEvent{answer}, nil, nil, nil)
	if err != nil || !slices.ContainsFunc(changes.Events, func(event SessionEvent) bool { return event.EventType == "session.status_running" }) {
		t.Fatalf("accept answer: %+v, %v", changes.Events, err)
	}
	if _, err := store.AppendSessionUserEvents(ctx, ref.WorkspaceUUID, harnessControlSessionID, []SessionEvent{answer}, nil, nil, nil); !errors.Is(err, ErrSessionToolUseNotPending) {
		t.Fatalf("second answer: %v", err)
	}
	answerRef := ref
	answerRef.UUID = answer.UUID
	continuation, found, err := store.HarnessResumeWork(ctx, answerRef, HarnessChoiceResponse)
	if err != nil || !found || continuation.Ref != ref || continuation.UUID != answer.UUID {
		t.Fatalf("continuation after answer: %+v, %t, %v", continuation, found, err)
	}
	if choices, err := state.GetPendingUserChoices(ctx, thread); err != nil || len(choices) != 0 {
		t.Fatalf("choice was not cleared: %+v, %v", choices, err)
	}
	conversation := store.HarnessConversation(HarnessTurnLease{Ref: ref})
	if found, err := conversation.HasMessageID(ctx, thread, continuation.ToolUUID+":result"); err != nil || found {
		t.Fatalf("choice was appended before the model returned: %t, %v", found, err)
	}

	next := HarnessTurnDelivery{InputUUID: answer.UUID, TaskUUID: continuation.UUID, ToolUUID: continuation.ToolUUID, Kind: continuation.Kind}
	if _, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{InputUUID: ref.UUID}, "stale-hash", time.Now().Add(time.Minute)); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("old input claimed a turn after it was resumed by the answer: %v", err)
	}
	claim, err = store.ClaimHarnessTurn(ctx, ref, next, "choice-hash", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	completeHarnessControlModel(t, store, claim, HarnessTool{CallID: "toolu_after_choice", Name: "Read", Arguments: json.RawMessage(`{}`)})
	history, err := conversation.LoadHistoryUpTo(ctx, thread, nil, nil)
	if err != nil || len(history) != 4 {
		t.Fatalf("history after answer: %+v, %v", history, err)
	}
	if result, ok := history[2].(harness.ToolResult); !ok || result.Failed || result.CallID != "toolu_pick" {
		t.Fatalf("unexpected conversation result: %+v", history[2])
	}
	continued, found, err := store.HarnessResumeWork(ctx, answerRef, HarnessChoiceResponse)
	if err != nil || !found || continued.Ref != ref || continued.Kind != HarnessToolTask || continued.UUID == answer.UUID {
		t.Fatalf("redelivery after the next model committed: %+v, %t, %v", continued, found, err)
	}
}

func TestHarnessSandboxResultWakesParkedTurnPostgres(t *testing.T) {
	ctx, database, store, initial := newHarnessControlTurn(t)
	ref := initial.Lease.Ref
	completeHarnessControlModel(t, store, initial, HarnessTool{CallID: "toolu_slow", Name: "Bash", Arguments: json.RawMessage(`{"command":"sleep 10"}`)})
	task, _, err := store.HarnessTurnTask(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	delivery := HarnessTurnDelivery{InputUUID: ref.UUID, TaskUUID: task.UUID, ToolUUID: task.ToolUUID, Kind: task.Kind}
	claim, err := store.ClaimHarnessTurn(ctx, ref, delivery, "dispatch-hash", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	scope := SandboxToolCallScope{
		OrganizationUUID: ref.OrganizationUUID, WorkspaceUUID: ref.WorkspaceUUID, CodeSessionID: claim.CodeSession.ExternalID,
		ThreadID: "root", CallID: task.ToolUUID, WorkerEpoch: claim.Lease.Epoch,
	}
	call, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{
		Scope: scope, Tool: "bash", Input: []byte(`{"command":"sleep 10"}`), Timeout: time.Minute, QueueTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ParkHarnessTool(ctx, initial.Lease, task.ToolUUID); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("stale owner parked the tool: %v", err)
	}
	if err := store.ParkHarnessTool(ctx, claim.Lease, task.ToolUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimHarnessTurn(ctx, ref, delivery, "old-input-hash", time.Now().Add(time.Minute)); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("original input reclaimed the parked tool: %v", err)
	}
	if due, err := store.ListReadyHarnessInputs(ctx, 64); err != nil || len(due) != 0 {
		t.Fatalf("unfinished sandbox call woke the turn: %+v, %v", due, err)
	}
	execClaimTest(t, ctx, database, `UPDATE sandbox_tool_calls SET state = 'succeeded', finished_at = now() WHERE uuid = $1`, call.UUID)
	wake := HarnessWake{Turn: ref, InputUUID: task.ToolUUID, Kind: HarnessToolResult}
	if due, err := store.ListReadyHarnessInputs(ctx, 64); err != nil || !slices.Equal(due, []HarnessWake{wake}) {
		t.Fatalf("missing sandbox completion wake: %+v, %v", due, err)
	}
	if err := store.MarkHarnessInputPublished(ctx, wake); err != nil {
		t.Fatal(err)
	}
	if due, err := store.ListReadyHarnessInputs(ctx, 64); err != nil || len(due) != 0 {
		t.Fatalf("already published wake: %+v, %v", due, err)
	}
	inputRef := ref
	inputRef.UUID = wake.InputUUID
	resume, found, err := store.HarnessResumeWork(ctx, inputRef, HarnessToolResult)
	if err != nil || !found || resume != task {
		t.Fatalf("resolve sandbox result: %+v, %t, %v", resume, found, err)
	}
	delivery.InputUUID = wake.InputUUID
	resumed, err := store.ClaimHarnessTurn(ctx, ref, delivery, "resumed-hash", time.Now().Add(time.Minute))
	if err != nil || resumed.Lease.Epoch <= claim.Lease.Epoch {
		t.Fatalf("recover tool under a new lease: %+v, %v", resumed, err)
	}
	if _, err := store.FinishHarnessTool(ctx, harnessControlSessionID, resumed.Lease, task.ToolUUID, sessioncontract.HarnessToolResult{Text: "done"}, nil); err != nil {
		t.Fatal(err)
	}
	continuation, found, err := store.HarnessResumeWork(ctx, inputRef, HarnessToolResult)
	if err != nil || !found || continuation.Kind != HarnessModelTask || continuation.ToolUUID != task.ToolUUID {
		t.Fatalf("tool result redelivery lost the model: %+v, %t, %v", continuation, found, err)
	}
}

func TestHarnessApprovalWaitAndResumePostgres(t *testing.T) {
	for _, decision := range []string{"deny", "allow"} {
		t.Run(decision, func(t *testing.T) {
			ctx, _, store, claim := newHarnessControlTurn(t)
			ref := claim.Lease.Ref
			now := time.Now().UTC()
			call := harness.ToolCall{ID: "toolu_bash", Name: "Bash", Arguments: `{"command":"echo ok"}`}
			use := SessionEvent{UUID: uuid.NewString(), ExternalID: maevents.ToolUseEventID(claim.CodeSession.ExternalID, string(call.ID), ""), EventType: "agent.tool_use", CreatedAt: now, ProcessedAt: now, Payload: json.RawMessage(`{"name":"bash","evaluated_permission":"ask"}`)}
			if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, nil, nil); err != nil {
				t.Fatal(err)
			}
			stored, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelEnd, []SessionEvent{use}, &HarnessModelCompletion{
				UserText: "Start", Message: harness.AssistantMessage{Call: call}, Pending: harness.ApprovalWork{Call: call}})
			if err != nil || !slices.ContainsFunc(stored, func(e SessionEvent) bool { return e.EventType == "session.status_idle" }) {
				t.Fatalf("park: %v, %v", stored, err)
			}
			task, state, err := store.HarnessTurnTask(ctx, ref)
			if err != nil || state != HarnessWaitingChoice {
				t.Fatalf("state %q: %v", state, err)
			}
			if _, err := store.ActiveHarnessTool(ctx, ref, task.UUID, claim.Lease.Epoch); !errors.Is(err, ErrNotFound) {
				t.Fatalf("tool available before approval: %v", err)
			}
			wrong := "sevt_wrong"
			confirmation := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_confirm", EventType: "user.tool_confirmation", CreatedAt: now.Add(time.Second), ToolUseID: &wrong,
				Payload: json.RawMessage(fmt.Sprintf(`{"result":%q,"tool_use_id":%q,"deny_message":"Use a different command"}`, decision, use.ExternalID))}
			appendConfirmation := func() error {
				_, err := store.AppendSessionUserEvents(ctx, ref.WorkspaceUUID, harnessControlSessionID, []SessionEvent{confirmation}, nil, nil, nil)
				return err
			}
			if err := appendConfirmation(); err == nil {
				t.Fatal("accepted unrelated confirmation")
			}
			confirmation.ToolUseID = &use.ExternalID
			if err := appendConfirmation(); err != nil {
				t.Fatal(err)
			}
			if err := appendConfirmation(); err == nil {
				t.Fatal("accepted duplicate confirmation")
			}
			resume := ref
			resume.UUID = confirmation.UUID
			next, found, err := store.HarnessResumeWork(ctx, resume, HarnessChoiceResponse)
			if err != nil || !found || next != task {
				t.Fatalf("resume: %+v, %v", next, err)
			}
			delivery := HarnessTurnDelivery{InputUUID: resume.UUID, TaskUUID: task.UUID, ToolUUID: task.ToolUUID, Kind: task.Kind}
			active, err := store.ClaimHarnessTurn(ctx, ref, delivery, "confirmed-hash", time.Now().Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			tool, err := store.ActiveHarnessTool(ctx, ref, task.UUID, active.Lease.Epoch)
			if err != nil || tool.Decision != decision || (decision == "deny" && tool.DenyMessage != "Use a different command") || tool.CallID != string(call.ID) {
				t.Fatalf("decision: %+v, %v", tool, err)
			}
		})
	}
}

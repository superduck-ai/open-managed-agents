//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"testing"
	"time"
	"uuid"
)

func TestDSHExecutionPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"'),worker_lease_expires_at=now()+interval '1 hour' WHERE external_id=$1`, id)
	code, _, err := store.GetCodeSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	scope := DSHScope{CodeSessionID: id, CodeSessionUUID: code.UUID, SessionID: code.SessionExternalID, OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, Epoch: 1}
	primary, err := store.CreateSessionThreadIfAbsent(ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "sthr_dsr", OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, SessionUUID: code.SessionUUID, SessionExternalID: code.SessionExternalID, AgentSnapshot: json.RawMessage(`{}`), Status: "running", Stats: json.RawMessage(`{}`), Usage: json.RawMessage(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("reject unknown input and stale owner", func(t *testing.T) {
		if _, _, err := store.FinishDSHInput(ctx, scope, "missing", DSHFinish{Result: "success"}); !errors.Is(err, ErrDSHInputNotEligible) {
			t.Fatalf("unknown finish=%v", err)
		}
		old := scope
		old.Epoch = 0
		if _, _, err := store.DeliverDSHInputs(ctx, old, []DSHDeliveryUpdate{{ID: "input", Status: "processing"}}); !errors.Is(err, ErrWorkerEpochMismatch) {
			t.Fatalf("stale=%v", err)
		}
	})
	event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "input", EventType: "user.message", Payload: json.RawMessage(`{"id":"input","type":"user.message","content":[{"type":"text","text":"hello"}]}`), CreatedAt: time.Now().UTC()}
	changes, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event}, nil)
	if err != nil || len(changes.Events) == 0 {
		t.Fatalf("accept=%+v,%v", changes, err)
	}
	t.Run("accepted turn requires processing and finish before ACK", func(t *testing.T) {
		if _, _, err := store.DeliverDSHInputs(ctx, scope, []DSHDeliveryUpdate{{ID: "input", Status: "processed"}}); !errors.Is(err, ErrDSHFinishRequired) {
			t.Fatalf("early ACK=%v", err)
		}
		if _, _, err := store.FinishDSHInput(ctx, scope, "input", DSHFinish{Result: "success"}); !errors.Is(err, ErrDSHInputNotEligible) {
			t.Fatalf("early finish=%v", err)
		}
		results, changes, err := store.DeliverDSHInputs(ctx, scope, []DSHDeliveryUpdate{{ID: "input", Status: "processing"}})
		if err != nil || results[0].ExecutionStatus != "running" || results[0].Outcome != "applied" || len(changes.Events) == 0 {
			t.Fatalf("processing=%+v,%+v,%v", results, changes, err)
		}
	})
	t.Run("repeated processing retains first start and tenant isolation", func(t *testing.T) {
		mapper := NewDSHExecutionMapper(store.mapperDB)
		before, found, err := mapper.Execution(ctx, scope, "input")
		if err != nil || !found || before.StartedAt == nil || before.CompletedAt != nil || before.Result != nil {
			t.Fatalf("running=%+v found=%v err=%v", before, found, err)
		}
		if _, _, err := store.DeliverDSHInputs(ctx, scope, []DSHDeliveryUpdate{{ID: "input", Status: "processing"}}); err != nil {
			t.Fatal(err)
		}
		after, _, err := mapper.Execution(ctx, scope, "input")
		if err != nil || after.StartedAt == nil || !after.StartedAt.Equal(*before.StartedAt) {
			t.Fatalf("start changed=%+v %v", after, err)
		}
		other := scope
		other.WorkspaceUUID = uuid.NewV4().String()
		if _, found, err := mapper.Execution(ctx, other, "input"); err != nil || found {
			t.Fatalf("cross-tenant found=%v err=%v", found, err)
		}
		summary, err := mapper.Summary(ctx, other)
		if err != nil || summary.ActiveInputID != nil || summary.LastResult != nil || summary.HasEligibleAccepted {
			t.Fatalf("cross-tenant summary=%+v %v", summary, err)
		}
	})

	header := []byte(`{"version":4,"id":"` + id + `","createdAt":0,"isSeeded":false}`)
	if _, err := store.PutDSHTranscript(ctx, scope, id, nil, header); err != nil {
		t.Fatal(err)
	}
	permission := DSHPermission{RequestID: "dsrp_1", InputID: "input", DSHSessionID: id, ThreadID: primary.ExternalID, ToolUseID: "call", Turn: "1", Tool: []byte(`{"kind":"builtin","name":"bash"}`), Input: []byte(`{"command":"true"}`), PublicEventID: "tool_public", State: "pending"}
	t.Run("permission publishes requires action and resolves in the input transaction", func(t *testing.T) {
		value, changes, err := store.RequestDSHPermission(ctx, scope, permission, nil)
		if err != nil || value.State != "pending" || len(changes.Events) == 0 {
			t.Fatalf("permission=%+v,%+v,%v", value, changes, err)
		}
		duplicate := permission
		duplicate.Input = []byte(`{"command":"false"}`)
		if _, _, err := store.RequestDSHPermission(ctx, scope, duplicate, nil); !errors.Is(err, ErrDSHPermissionConflict) {
			t.Fatalf("changed=%v", err)
		}
		confirm := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "confirm", EventType: "user.tool_confirmation", Payload: json.RawMessage(`{"type":"user.tool_confirmation","tool_use_id":"tool_public","result":"allow"}`), CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}
		second := confirm
		second.UUID, second.ExternalID = uuid.NewV4().String(), "contradictory-confirm"
		second.Payload = []byte(`{"type":"user.tool_confirmation","tool_use_id":"tool_public","result":"deny"}`)
		if _, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{confirm, second}, nil); !errors.Is(err, ErrSessionToolUseNotPending) {
			t.Fatalf("contradictory confirmation batch=%v", err)
		}
		if _, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{confirm}, nil); err != nil {
			t.Fatal(err)
		}
		value, found, err := store.GetDSHPermission(ctx, scope, permission.RequestID)
		if err != nil || !found || value.State != "resolved" || len(value.Decision) == 0 {
			t.Fatalf("resolved=%+v,%v", value, err)
		}
		inherited, _, err := store.RequestDSHPermission(ctx, scope, permission, nil)
		if err != nil || inherited.State != "resolved" {
			t.Fatalf("inherited=%+v,%v", inherited, err)
		}
	})
	t.Run("cancel before create leaves an immutable tombstone", func(t *testing.T) {
		tombstone := permission
		tombstone.RequestID = "dsrp_cancel"
		tombstone.ToolUseID = "cancel-call"
		value, _, err := store.CancelDSHPermission(ctx, scope, tombstone)
		if err != nil || value.State != "cancelled" {
			t.Fatalf("cancel=%+v,%v", value, err)
		}
		value, _, err = store.RequestDSHPermission(ctx, scope, tombstone, nil)
		if err != nil || value.State != "cancelled" {
			t.Fatalf("created after cancel=%+v,%v", value, err)
		}
	})
	t.Run("public receipt and events commit atomically", func(t *testing.T) {
		malformed := DSHPublicEvent{ID: "malformed", Payload: []byte(`{"a":1,"a":2}`)}
		if _, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{malformed}); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("invalid canonical input=%v", err)
		}
		now := time.Now().UTC()
		first := DSHPublicEvent{ID: "answer", Payload: []byte(`{"id":"answer","type":"agent.message","content":[]}`), Events: []SessionEvent{{UUID: uuid.NewV4().String(), ExternalID: "answer", EventType: "agent.message", Payload: []byte(`{"type":"agent.message","content":[]}`), CreatedAt: now, ProcessedAt: now}}}
		invalid := first
		invalid.ID = "bad"
		invalid.Events = append([]SessionEvent(nil), first.Events...)
		thread := "missing"
		invalid.Events[0].ThreadExternalID = &thread
		invalid.Events[0].ExternalID = "bad"
		if _, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{first, invalid}); err == nil {
			t.Fatal("partial batch committed")
		}
		result, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{first})
		if err != nil || result.Accepted != 1 {
			t.Fatalf("publish=%+v,%v", result, err)
		}
		first.Payload = []byte(`{ "content":[], "type":"agent.message", "id":"answer" }`)
		result, err = store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{first})
		if err != nil || result.Duplicates != 1 {
			t.Fatalf("duplicate=%+v,%v", result, err)
		}
		digest, found, err := NewDSHExecutionMapper(store.mapperDB).Receipt(ctx, scope, first.ID)
		expected, digestErr := jsoncanonical.Digest(first.Payload)
		if err != nil || digestErr != nil || !found || digest != expected {
			t.Fatalf("stored digest=%q,%v,%v", digest, found, err)
		}
		first.Payload = []byte(`{"id":"answer","type":"agent.message","content":[1]}`)
		if _, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{first}); !errors.Is(err, ErrDSHPublicConflict) {
			t.Fatalf("changed output=%v", err)
		}
	})
	t.Run("schema rejects invalid completion shapes without advancing execution", func(t *testing.T) {
		code := "execution_failed"
		for _, completion := range []DSHFinish{{Result: "recovered", FailureCode: &code}, {Result: "failed"}, {Result: "success", FailureCode: &code}} {
			if _, _, err := store.FinishDSHInput(ctx, scope, "input", completion); err == nil {
				t.Fatalf("accepted invalid completion=%+v", completion)
			}
		}
		row, _, err := NewDSHExecutionMapper(store.mapperDB).Execution(ctx, scope, "input")
		if err != nil || row.Status != "running" || row.CompletedAt != nil {
			t.Fatalf("invalid completion persisted=%+v %v", row, err)
		}
	})

	t.Run("finish is immutable and terminal input is acknowledged", func(t *testing.T) {
		if _, err := store.RecordDSHDelivery(ctx, scope, "input", "durable-ack", ""); err != nil {
			t.Fatal(err)
		}
		result, _, err := store.FinishDSHInput(ctx, scope, "input", DSHFinish{Result: "success"})
		if err != nil || result.Result != "success" {
			t.Fatalf("finish=%+v,%v", result, err)
		}
		if _, _, err := store.FinishDSHInput(ctx, scope, "input", DSHFinish{Result: "success"}); err != nil {
			t.Fatal(err)
		}
		cancelled, _, err := store.CancelDSHPermission(ctx, scope, DSHPermission{RequestID: "dsrp_cancel", InputID: "input"})
		if err != nil || cancelled.State != "cancelled" {
			t.Fatalf("cancel replay after finish=%+v,%v", cancelled, err)
		}
		resolved, _, err := store.CancelDSHPermission(ctx, scope, permission)
		if err != nil || resolved.State != "resolved" {
			t.Fatalf("resolved approval changed after finish=%+v,%v", resolved, err)
		}
		if _, _, err := store.FinishDSHInput(ctx, scope, "input", DSHFinish{Result: "cancelled"}); !errors.Is(err, ErrDSHFinishConflict) {
			t.Fatalf("changed finish=%v", err)
		}
		results, _, err := store.DeliverDSHInputs(ctx, scope, []DSHDeliveryUpdate{{ID: "input", Status: "processed"}})
		if err != nil || results[0].ExecutionStatus != "completed" {
			t.Fatalf("ACK=%+v,%v", results, err)
		}
		row, found, err := NewDSHDeliveryMapper(store.mapperDB).Find(ctx, scope, "input")
		if err != nil || !found || row.Status != "pending" {
			t.Fatalf("pending asynchronous ACK=%+v,%v", row, err)
		}
	})
	t.Run("interrupt survives control delivery before task replay", func(t *testing.T) {
		message := event
		message.UUID, message.ExternalID = uuid.NewV4().String(), "cancelled-input"
		message.CreatedAt = time.Now().UTC()
		if _, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{message}, nil); err != nil {
			t.Fatal(err)
		}
		interrupt := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "interrupt", EventType: "user.interrupt", Payload: []byte(`{"type":"user.interrupt"}`), CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}
		if _, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{interrupt}, nil); err != nil {
			t.Fatal(err)
		}
		results, _, err := store.DeliverDSHInputs(ctx, scope, []DSHDeliveryUpdate{{ID: "cancelled-input", Status: "processing"}})
		if err != nil || results[0].CancelRequestedAt == nil {
			t.Fatalf("durable interrupt=%+v,%v", results, err)
		}
		if _, _, err := store.FinishDSHInput(ctx, scope, "cancelled-input", DSHFinish{Result: "cancelled"}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("failed result pauses queue until a newly accepted input wakes it", func(t *testing.T) {
		mapper := NewDSHExecutionMapper(store.mapperDB)
		for _, inputID := range []string{"failure", "queued"} {
			if _, err := mapper.InsertExecution(ctx, scope, inputID, primary.ExternalID, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := store.DeliverDSHInputs(ctx, scope, []DSHDeliveryUpdate{{ID: "failure", Status: "processing"}}); err != nil {
			t.Fatal(err)
		}
		failureCode := "execution_failed"
		if _, _, err := store.FinishDSHInput(ctx, scope, "failure", DSHFinish{Result: "failed", FailureCode: &failureCode}); err != nil {
			t.Fatal(err)
		}
		different := "interrupted_recovery"
		if _, _, err := store.FinishDSHInput(ctx, scope, "failure", DSHFinish{Result: "failed", FailureCode: &different}); !errors.Is(err, ErrDSHFinishConflict) {
			t.Fatalf("changed failure code=%v", err)
		}
		summary, err := mapper.Summary(ctx, scope)
		if err != nil || summary.HasEligibleAccepted || summary.LastResult == nil || *summary.LastResult != "failed" {
			t.Fatalf("failed summary=%+v %v", summary, err)
		}
		if _, _, err := store.DeliverDSHInputs(ctx, scope, []DSHDeliveryUpdate{{ID: "queued", Status: "processing"}}); !errors.Is(err, ErrDSHInputNotEligible) {
			t.Fatalf("old queue=%v", err)
		}
		if _, err := mapper.InsertExecution(ctx, scope, "new-input", primary.ExternalID, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		summary, err = mapper.Summary(ctx, scope)
		if err != nil || !summary.HasEligibleAccepted {
			t.Fatalf("new summary=%+v %v", summary, err)
		}
		if _, _, err := store.DeliverDSHInputs(ctx, scope, []DSHDeliveryUpdate{{ID: "queued", Status: "processing"}}); err != nil {
			t.Fatalf("new input did not wake ordered queue: %v", err)
		}

	})

	t.Run("internal termination retains normal payload preparation and cleanup", func(t *testing.T) {
		event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "terminated", EventType: "session.status_terminated", CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}
		changes, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event}, nil)
		if err != nil || len(changes.RetiredCodeSessionIDs) != 1 {
			t.Fatalf("terminate=%+v,%v", changes, err)
		}
		current, found, err := store.GetSession(ctx, scope.WorkspaceUUID, scope.SessionID)
		if err != nil || !found || current.Status != "terminated" {
			t.Fatalf("terminated session=%+v,%v", current, err)
		}
	})

}

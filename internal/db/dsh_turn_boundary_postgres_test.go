//go:build integration

package db

import (
	"testing"
	"time"
	"uuid"
)

func newDSRTurnBoundaryFixture(t *testing.T) (dshLockFixture, SessionThread) {
	t.Helper()
	f := newDSHLockFixture(t)
	execClaimTest(t, f.ctx, f.database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"') WHERE uuid=$1`, f.code.UUID)
	thread, err := f.store.CreateSessionThreadIfAbsent(f.ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "sthr_completion", OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, SessionUUID: f.code.SessionUUID, SessionExternalID: f.code.SessionExternalID, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return f, thread
}

func TestDSHFinishPublishesIdleBeforeQueuedClaimPostgres(t *testing.T) {
	f, _ := newDSRTurnBoundaryFixture(t)
	for _, id := range []string{"input1", "input2"} {
		event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: id, EventType: "user.message", Payload: []byte(`{"id":"` + id + `","type":"user.message","content":[{"type":"text","text":"hi"}]}`), CreatedAt: time.Now().UTC()}
		if _, err := f.store.AppendSessionEvents(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, []SessionEvent{event}, nil); err != nil {
			t.Fatal(err)
		}
	}
	claim, _, err := f.store.ClaimDSHInput(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	_, changes, err := f.store.FinishDSHInput(f.ctx, f.scope, claim.Event.ExternalID, DSHFinish{Result: "cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	idle := false
	for _, event := range changes.Events {
		idle = idle || event.EventType == "session.status_idle"
	}
	if !idle {
		t.Fatal("queued input hid completed turn idle")
	}
	next, nextChanges, err := f.store.ClaimDSHInput(f.ctx, f.scope)
	if len(nextChanges.Events) != 3 || nextChanges.Events[0].EventType != "user.message" || nextChanges.Events[1].EventType != "session.status_running" || nextChanges.Events[2].EventType != "session.thread_status_running" {
		t.Fatalf("wrong next turn order: %+v", nextChanges.Events)
	}
	if err != nil || next.Event.ExternalID == "" || next.Event.ExternalID == claim.Event.ExternalID {
		t.Fatalf("next claim: %+v %v", next, err)
	}
}

//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"
)

func TestDSHInputClaimsPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"'),worker_lease_expires_at=now()+interval '1 hour' WHERE external_id=$1`, id)
	code, _, err := store.GetCodeSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	scope := DSHScope{CodeSessionID: id, CodeSessionUUID: code.UUID, SessionID: code.SessionExternalID, OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, Epoch: 1}
	_, err = store.CreateSessionThreadIfAbsent(ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "sthr_claim", OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, SessionUUID: code.SessionUUID, SessionExternalID: code.SessionExternalID, AgentSnapshot: json.RawMessage(`{}`), Status: "running", Stats: json.RawMessage(`{}`), Usage: json.RawMessage(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*DSHScope){func(s *DSHScope) { s.Epoch = 0 }, func(s *DSHScope) { s.OrganizationUUID = uuid.NewV4().String() }, func(s *DSHScope) { s.WorkspaceUUID = uuid.NewV4().String() }} {
		wrong := scope
		mutate(&wrong)
		if _, _, err := store.ClaimDSHInput(ctx, wrong); err == nil {
			t.Fatal("invalid scope claimed input")
		}
	}
	if claim, _, err := store.ClaimDSHInput(ctx, scope); err != nil || claim.Event.ExternalID != "" {
		t.Fatalf("empty claim=%+v %v", claim, err)
	}
	now := time.Now().UTC()
	for _, inputID := range []string{"b", "a"} {
		event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: inputID, EventType: "user.message", Payload: json.RawMessage(`{"type":"user.message","content":[]}`), CreatedAt: now}
		if _, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			claim, _, err := store.ClaimDSHInput(ctx, scope)
			if err != nil || claim.Event.ExternalID != "a" {
				t.Errorf("retry claim=%+v %v", claim, err)
			}
		})
	}
	group.Wait()
	before, _, err := NewDSHExecutionMapper(store.mapperDB).Execution(ctx, scope, "a")
	if err != nil || before.Status != "running" || before.StartedAt == nil {
		t.Fatalf("running=%+v %v", before, err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=2 WHERE external_id=$1`, id)
	if _, _, err := store.FinishDSHInput(ctx, scope, "a", DSHFinish{Result: "success"}); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("stale finish=%v", err)
	}
	scope.Epoch = 2
	recovered, _, err := store.ClaimDSHInput(ctx, scope)
	if err != nil || recovered.Event.ExternalID != "a" {
		t.Fatalf("recover=%+v %v", recovered, err)
	}
	after, _, err := NewDSHExecutionMapper(store.mapperDB).Execution(ctx, scope, "a")
	if err != nil || after.StartedAt == nil || !after.StartedAt.Equal(*before.StartedAt) {
		t.Fatalf("changed start=%+v %v", after, err)
	}
	if _, _, err := store.FinishDSHInput(ctx, scope, "a", DSHFinish{Result: "success"}); err != nil {
		t.Fatal(err)
	}
	next, _, err := store.ClaimDSHInput(ctx, scope)
	if err != nil || next.Event.ExternalID != "b" {
		t.Fatalf("next=%+v %v", next, err)
	}
}

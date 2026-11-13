//go:build integration

package db

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSandboxToolCallsPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeID := seedClaimCodeSession(t, ctx, database, 91, "claim", "idle")
	work, sandbox := uuid.NewString(), uuid.NewString()
	execClaimTest(t, ctx, database, `INSERT INTO environment_work(uuid,external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,session_uuid,state) SELECT $1,'work_tool_claim',organization_uuid,workspace_uuid,environment_uuid,environment_external_id,session_uuid,'active' FROM code_sessions WHERE external_id=$2`, work, codeID)
	execClaimTest(t, ctx, database, `INSERT INTO environment_sandboxes(uuid,external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,work_uuid,work_external_id,provider,template,provider_sandbox_id,state) SELECT $1,'sandbox_tool_claim',organization_uuid,workspace_uuid,environment_uuid,environment_external_id,$2,'work_tool_claim','e2b','test','provider-tool-claim','running' FROM code_sessions WHERE external_id=$3`, sandbox, work, codeID)
	scope := SandboxToolCallScope{OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, CodeSessionID: codeID, WorkerEpoch: 1, ThreadID: "root", CallID: "call", SandboxUUID: sandbox}
	call, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: scope, Tool: "bash", Input: []byte(`{"command":"true"}`), QueueTimeout: time.Minute, Timeout: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	foreign := scope
	foreign.WorkspaceUUID = uuid.NewString()
	if _, err := store.GetSandboxToolCall(ctx, foreign); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("foreign lookup: %v", err)
	}
	var group sync.WaitGroup
	var lock sync.Mutex
	count := 0
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			calls, err := store.ClaimSandboxToolCalls(ctx, scope, 1)
			lock.Lock()
			defer lock.Unlock()
			if err != nil {
				t.Error(err)
			}
			count += len(calls)
		}()
	}
	group.Wait()
	if count != 1 {
		t.Fatalf("claimed %d times", count)
	}
	got, err := store.GetSandboxToolCall(ctx, scope)
	if err != nil || got.State != "dispatched" || got.Attempt != 1 {
		t.Fatalf("claim: %+v %v", got, err)
	}
	if _, err := store.ReportSandboxToolCall(ctx, SandboxToolCallReport{Scope: scope, CallUUID: call.UUID, Attempt: 2, Event: "started"}); !errors.Is(err, ErrToolCallConflict) {
		t.Fatalf("wrong attempt accepted: %v", err)
	}

	if _, err := store.ReportSandboxToolCall(ctx, SandboxToolCallReport{Scope: scope, CallUUID: call.UUID, Attempt: 1, Event: "reject"}); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := store.ClaimSandboxToolCalls(ctx, scope, 1)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].Attempt != 2 {
		t.Fatalf("reclaim: %+v %v", reclaimed, err)
	}
	started := time.Now()
	if _, err := store.ReportSandboxToolCall(ctx, SandboxToolCallReport{Scope: scope, CallUUID: call.UUID, Attempt: 2, Event: "started"}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetSandboxToolCall(ctx, scope); err != nil || got.TimeoutMS == nil || *got.TimeoutMS != 600000 || got.DeadlineAt.Before(started.Add(10*time.Minute-time.Second)) {
		t.Fatalf("execution deadline: %+v %v", got, err)
	}
	if got, err := store.ReportSandboxToolCall(ctx, SandboxToolCallReport{Scope: scope, CallUUID: call.UUID, Attempt: 1, Event: "reject"}); err != nil || got.State != "running" {
		t.Fatalf("late reject: %+v %v", got, err)
	}
	child := scope
	child.ThreadID = "child"
	if _, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: child, Tool: "read", Input: []byte(`{"file_path":"/tmp/a"}`), QueueTimeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.CancelSandboxToolCall(ctx, child); err != nil || got.State != "cancelled" {
		t.Fatalf("child cancel: %+v %v", got, err)
	}
	tombstone := scope
	tombstone.CallID = "cancel-before-submit"
	if _, err := store.CancelSandboxToolCall(ctx, tombstone); err != nil {
		t.Fatal(err)
	}
	if got, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: tombstone, Tool: "bash", Input: []byte(`{"command":"never"}`), QueueTimeout: time.Minute}); err != nil || got.State != "cancelled" {
		t.Fatalf("late submit: %+v %v", got, err)
	}
	expired := scope
	expired.CallID = "expired"
	if _, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: expired, Tool: "bash", Input: []byte(`{}`), QueueTimeout: -time.Second}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.ClaimSandboxToolCalls(ctx, scope, 1); err != nil || len(claimed) != 0 {
		t.Fatalf("expired claimed: %+v %v", claimed, err)
	}
	if _, err := store.SweepSandboxToolCalls(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetSandboxToolCall(ctx, expired); err != nil || got.State != "expired" {
		t.Fatalf("expiry: %+v %v", got, err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=2 WHERE external_id=$1`, codeID)
	if _, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: scope, Tool: "bash", Input: []byte(`{}`), QueueTimeout: time.Minute}); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("stale owner: %v", err)
	}
	scope.WorkerEpoch = 2
	database.SetMaxOpenConns(1)
	validationScope := scope
	validationScope.CallID = "single-connection-validation"
	validationCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := store.SubmitSandboxToolCall(validationCtx, SandboxToolCallSubmission{Scope: validationScope, Tool: "read", Input: []byte(`{}`), QueueTimeout: time.Minute, Validate: func(ctx context.Context) (SandboxToolCallValidation, error) {
		_, _, err := store.GetMemoryByPath(ctx, claimTestWorkspace, "missing", "/missing")
		return SandboxToolCallValidation{}, err
	}}); err != nil {
		t.Fatalf("validation escaped transaction: %v", err)
	}
	uncommitted := scope
	uncommitted.CallID = "dispatched-at-retirement"
	if _, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: uncommitted, Tool: "read", Input: []byte(`{"file_path":"/tmp/a"}`), QueueTimeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimSandboxToolCalls(ctx, scope, 7)
	if err != nil || !slices.ContainsFunc(claimed, func(call SandboxToolCall) bool { return call.ExternalID == uncommitted.CallID }) {
		t.Fatalf("claim before retirement: %+v %v", claimed, err)
	}
	execClaimTest(t, ctx, database, `UPDATE environment_sandboxes SET state='stopping' WHERE uuid=$1`, sandbox)
	got, err = store.GetSandboxToolCall(ctx, scope)
	if err != nil || got.State != "lost" || got.ErrorCode == nil || *got.ErrorCode != "executor_lost" {
		t.Fatalf("retirement: %+v %v", got, err)
	}
	got, err = store.GetSandboxToolCall(ctx, uncommitted)
	if err != nil || got.State != "pending" || got.SandboxUUID != nil || got.LeaseExpiresAt != nil {
		t.Fatalf("uncommitted retirement: %+v %v", got, err)
	}
}

func TestSandboxToolCallClaimsDistinguishPreparingAndRetiredSandboxes(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeID := seedClaimCodeSession(t, ctx, database, 92, "claim", "idle")
	work, sandbox := uuid.NewString(), uuid.NewString()
	execClaimTest(t, ctx, database, `INSERT INTO environment_work(uuid,external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,session_uuid,state) SELECT $1,'work_tool_ready',organization_uuid,workspace_uuid,environment_uuid,environment_external_id,session_uuid,'active' FROM code_sessions WHERE external_id=$2`, work, codeID)
	execClaimTest(t, ctx, database, `INSERT INTO environment_sandboxes(uuid,external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,work_uuid,work_external_id,provider,template,provider_sandbox_id,state) SELECT $1,'sandbox_tool_ready',organization_uuid,workspace_uuid,environment_uuid,environment_external_id,$2,'work_tool_ready','e2b','test','provider-tool-ready','creating' FROM code_sessions WHERE external_id=$3`, sandbox, work, codeID)
	scope := SandboxToolCallScope{OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, CodeSessionID: codeID, SandboxUUID: sandbox}
	readyAt := func() *time.Time {
		var ready *time.Time
		if err := database.QueryRowContext(ctx, `SELECT tool_call_ready_at FROM environment_sandboxes WHERE uuid=$1`, sandbox).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		return ready
	}

	execClaimTest(t, ctx, database, `UPDATE environment_sandboxes SET state='failed' WHERE uuid=$1`, sandbox)
	if _, err := store.ClaimSandboxToolCalls(ctx, scope, 1); !errors.Is(err, ErrToolCallSandboxRetired) {
		t.Fatalf("failed sandbox claim: %v", err)
	}
	if readyAt() != nil {
		t.Fatal("retired sandbox was marked ready")
	}
	execClaimTest(t, ctx, database, `UPDATE environment_sandboxes SET state='creating' WHERE uuid=$1`, sandbox)

	if _, err := store.ClaimSandboxToolCalls(ctx, scope, 1); !errors.Is(err, ErrToolCallSandboxPreparing) {
		t.Fatalf("preparing claim: %v", err)
	}
	first := readyAt()
	if first == nil {
		t.Fatal("preparing claim did not mark readiness")
	}
	if ready, err := store.IsSandboxToolCallReady(ctx, claimTestOrganization, claimTestWorkspace, sandbox); err != nil || !ready {
		t.Fatalf("runner readiness: %v %v", ready, err)
	}
	if _, err := store.ClaimSandboxToolCalls(ctx, scope, 1); !errors.Is(err, ErrToolCallSandboxPreparing) {
		t.Fatalf("repeated preparing claim: %v", err)
	}
	execClaimTest(t, ctx, database, `UPDATE environment_sandboxes SET state='running' WHERE uuid=$1`, sandbox)
	if calls, err := store.ClaimSandboxToolCalls(ctx, scope, 1); err != nil || len(calls) != 0 {
		t.Fatalf("running claim: %+v %v", calls, err)
	}
	if again := readyAt(); again == nil || !again.Equal(*first) {
		t.Fatalf("readiness rewritten: first=%v again=%v", first, again)
	}
	execClaimTest(t, ctx, database, `UPDATE environment_work SET state='queued' WHERE uuid=$1`, work)
	if _, err := store.ClaimSandboxToolCalls(ctx, scope, 1); !errors.Is(err, ErrToolCallSandboxPreparing) {
		t.Fatalf("running sandbox with inactive work: %v", err)
	}
	execClaimTest(t, ctx, database, `UPDATE environment_work SET state='active' WHERE uuid=$1`, work)
	execClaimTest(t, ctx, database, `UPDATE environment_sandboxes SET state='stopping' WHERE uuid=$1`, sandbox)
	if _, err := store.ClaimSandboxToolCalls(ctx, scope, 1); !errors.Is(err, ErrToolCallSandboxRetired) {
		t.Fatalf("retired claim: %v", err)
	}
}

func TestSandboxToolCallMaintenanceSplitsWorkAcrossInstances(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeID := seedClaimCodeSession(t, ctx, database, 93, "claim", "idle")
	scope := SandboxToolCallScope{OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, CodeSessionID: codeID, WorkerEpoch: 1, ThreadID: "root"}
	const expired = 150
	for index := range expired {
		call := scope
		call.CallID = "expired-" + strconv.Itoa(index)
		if _, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: call, Tool: "bash", Input: []byte(`{}`), QueueTimeout: -time.Second}); err != nil {
			t.Fatal(err)
		}
	}
	finalizing := scope
	finalizing.CallID = "finalizing"
	if _, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: finalizing, Tool: "write", Input: []byte(`{}`), QueueTimeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `UPDATE sandbox_tool_calls SET state='finalizing', finalizer='memory_version', finalize_until=NOW() - INTERVAL '1 second' WHERE external_id='finalizing'`)

	var group sync.WaitGroup
	var lock sync.Mutex
	swept := map[string]int{}
	finalized := 0
	for range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			changes, err := store.SweepSandboxToolCalls(ctx)
			calls, finalizeErr := store.ClaimDueSandboxToolCallFinalizations(ctx)
			lock.Lock()
			defer lock.Unlock()
			if err != nil || finalizeErr != nil {
				t.Error(err, finalizeErr)
			}
			for _, change := range changes {
				swept[change.ExternalID]++
			}
			finalized += len(calls)
		}()
	}
	group.Wait()
	if len(swept) != expired {
		t.Fatalf("swept %d distinct calls, want %d", len(swept), expired)
	}
	for id, count := range swept {
		if count != 1 {
			t.Fatalf("call %s swept %d times", id, count)
		}
	}
	if finalized != 1 {
		t.Fatalf("finalization claimed %d times", finalized)
	}
	var pending, extended int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE state <> 'expired' AND external_id LIKE 'expired-%'), count(*) FILTER (WHERE external_id = 'finalizing' AND finalize_until > NOW() + INTERVAL '30 seconds') FROM sandbox_tool_calls`).Scan(&pending, &extended); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || extended != 1 {
		t.Fatalf("unexpired=%d finalize lease extended=%d", pending, extended)
	}
}

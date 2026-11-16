//go:build integration

package db

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestManagerResumePostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeID := seedClaimCodeSession(t, ctx, database, 301, "claim", "idle")
	work, sandbox := uuid.NewString(), uuid.NewString()
	execClaimTest(t, ctx, database, `INSERT INTO environment_work(uuid,external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,session_uuid,state) SELECT $1,'work_resume',organization_uuid,workspace_uuid,environment_uuid,environment_external_id,session_uuid,'active' FROM code_sessions WHERE external_id=$2`, work, codeID)
	execClaimTest(t, ctx, database, `INSERT INTO environment_sandboxes(uuid,external_id,organization_uuid,workspace_uuid,environment_uuid,environment_external_id,work_uuid,work_external_id,provider,template,provider_sandbox_id,state) SELECT $1,'sandbox_resume',organization_uuid,workspace_uuid,environment_uuid,environment_external_id,$2,'work_resume','e2b','test','provider-resume','running' FROM code_sessions WHERE external_id=$3`, sandbox, work, codeID)
	target, found, err := store.FindManagerResumeTarget(ctx, claimTestWorkspace, "provider-resume")
	if err != nil || !found || target.Transport != "claim" || target.Phase != "" {
		t.Fatalf("target=%+v found=%v error=%v", target, found, err)
	}
	if _, found, err := store.FindManagerResumeTarget(ctx, uuid.NewString(), "provider-resume"); err != nil || found {
		t.Fatalf("foreign lookup: %v %v", found, err)
	}
	foreign := target
	foreign.OrganizationUUID = uuid.NewString()
	if err := store.SetManagerResumePhase(ctx, foreign, "checking"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("foreign update=%v", err)
	}
	scope := SandboxToolCallScope{OrganizationUUID: target.OrganizationUUID, WorkspaceUUID: target.WorkspaceUUID, CodeSessionID: codeID, WorkerEpoch: 1, ThreadID: "root", CallID: "first", SandboxUUID: sandbox}
	call, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: scope, Tool: "bash", Input: []byte(`{}`), QueueTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimSandboxToolCalls(ctx, scope, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claims=%v error=%v", claimed, err)
	}
	if err := store.SetManagerResumePhase(ctx, target, "checking"); err != nil {
		t.Fatal(err)
	}
	if idle, err := store.IsManagerResumeIdle(ctx, target); err != nil || idle {
		t.Fatalf("active call considered idle: %v %v", idle, err)
	}
	scope.CallID = "queued"
	if _, err := store.SubmitSandboxToolCall(ctx, SandboxToolCallSubmission{Scope: scope, Tool: "bash", Input: []byte(`{}`), QueueTimeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if calls, err := store.ClaimSandboxToolCalls(ctx, scope, 1); err != nil || len(calls) != 0 {
		t.Fatalf("claims during resume=%v %v", calls, err)
	}
	if _, err := store.ReportSandboxToolCall(ctx, SandboxToolCallReport{Scope: scope, CallUUID: call.UUID, Attempt: 1, Event: "succeeded", Result: []byte(`{"content":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if idle, err := store.IsManagerResumeIdle(ctx, target); err != nil || !idle {
		t.Fatalf("finished call not idle: %v %v", idle, err)
	}
	if err := store.WithManagerResumeLock(ctx, sandbox, func() error {
		err := store.WithManagerResumeLock(ctx, sandbox, func() error { t.Error("concurrent upgrade entered"); return nil })
		if !errors.Is(err, ErrPreconditionFailed) {
			t.Fatalf("concurrent lock=%v", err)
		}
		return store.SetManagerResumePhase(ctx, target, "starting")
	}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("interrupted resume")
	if err := store.WithManagerResumeLock(ctx, sandbox, func() error {
		if err := store.SetManagerResumePhase(ctx, target, "starting"); err != nil {
			return err
		}
		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("interruption=%v", err)
	}
	if current, _, err := store.FindManagerResumeTarget(ctx, claimTestWorkspace, "provider-resume"); err != nil || current.Phase != "starting" {
		t.Fatalf("lost resume state: %+v %v", current, err)
	}
	if ready, err := store.IsSandboxToolCallReady(ctx, target.OrganizationUUID, target.WorkspaceUUID, sandbox); err != nil || ready {
		t.Fatalf("stale ready=%v %v", ready, err)
	}
	if _, err := store.ClaimSandboxToolCalls(ctx, scope, 0); err != nil {
		t.Fatal(err)
	}
	if ready, err := store.IsSandboxToolCallReady(ctx, target.OrganizationUUID, target.WorkspaceUUID, sandbox); err != nil || !ready {
		t.Fatalf("new ready=%v %v", ready, err)
	}
	if err := store.RecordManagerVersion(ctx, target, "0.1.1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetManagerResumePhase(ctx, target, ""); err != nil {
		t.Fatal(err)
	}
	if calls, err := store.ClaimSandboxToolCalls(ctx, scope, 1); err != nil || len(calls) != 1 {
		t.Fatalf("claims after resume=%v %v", calls, err)
	}
	if target, _, err := store.FindManagerResumeTarget(ctx, claimTestWorkspace, "provider-resume"); err != nil || target.Version != "0.1.1" || target.Phase != "" {
		t.Fatalf("persisted target=%+v %v", target, err)
	}
}

package db

import (
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSandboxToolCallRejectCannotUndoRunningAttempt(t *testing.T) {
	now := time.Now().UTC()
	sandbox := "sandbox"
	row := sandboxToolCallRow{State: "running", Attempt: 2, SandboxUUID: &sandbox, DeadlineAt: now.Add(time.Minute)}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "reject"}, now); err != nil || row.State != "running" {
		t.Fatalf("running reject: %+v %v", row, err)
	}
}

func TestSandboxToolCallInterruptedRequiresKnownReason(t *testing.T) {
	now := time.Now().UTC()
	row := sandboxToolCallRow{State: "running", DeadlineAt: now.Add(time.Minute)}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "interrupted", Reason: "unknown"}, now); !errors.Is(err, ErrToolCallConflict) {
		t.Fatalf("unknown reason accepted: %+v %v", row, err)
	}
}

func TestSandboxToolCallDeadlineExpiresDispatchBeforeCommit(t *testing.T) {
	now := time.Now().UTC()
	row := sandboxToolCallRow{State: "dispatched", DeadlineAt: now.Add(-time.Second)}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "started"}, now); err != nil || row.State != "expired" {
		t.Fatalf("expired dispatch started: %+v %v", row, err)
	}
	if row.CancelReason == nil || *row.CancelReason != "deadline" {
		t.Fatal("missing deadline stop reason")
	}
}

func TestSandboxToolCallQueueDeadlineDoesNotLimitExecution(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(time.Second)
	timeout := int64(600000)
	row := sandboxToolCallRow{State: "dispatched", DeadlineAt: now.Add(time.Second), TimeoutMS: &timeout, LeaseExpiresAt: &lease}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "started"}, now); err != nil || row.State != "running" {
		t.Fatalf("started: %+v %v", row, err)
	}
	if !row.DeadlineAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("execution deadline=%v want start plus timeout", row.DeadlineAt)
	}
	later := now.Add(20 * time.Second)
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "heartbeat"}, later); err != nil || row.State != "running" {
		t.Fatalf("queue deadline stopped execution: %+v %v", row, err)
	}
}

func TestSandboxToolCallWithoutTimeoutKeepsSubmittedDeadline(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(time.Second)
	deadline := now.Add(time.Minute)
	row := sandboxToolCallRow{State: "dispatched", DeadlineAt: deadline, LeaseExpiresAt: &lease}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "started"}, now); err != nil || !row.DeadlineAt.Equal(deadline) {
		t.Fatalf("legacy deadline changed: %+v %v", row, err)
	}
}

func TestSandboxToolCallExpiredDispatchLeaseRequeuesAndRejectsLateStart(t *testing.T) {
	now := time.Now().UTC()
	expired := now.Add(-time.Second)
	sandbox := "sandbox"
	row := sandboxToolCallRow{State: "dispatched", Attempt: 1, SandboxUUID: &sandbox, DeadlineAt: now.Add(time.Minute), LeaseExpiresAt: &expired}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "started"}, now); !errors.Is(err, ErrToolCallConflict) {
		t.Fatalf("late start accepted: %+v %v", row, err)
	}
	if row.State != "pending" || row.SandboxUUID != nil || row.LeaseExpiresAt != nil {
		t.Fatalf("dispatch not requeued: %+v", row)
	}
}

func TestSandboxToolCallRunningLeaseLossIsLost(t *testing.T) {
	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	row := sandboxToolCallRow{State: "running", DeadlineAt: now.Add(time.Minute), LeaseExpiresAt: &expired}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "heartbeat"}, now); err != nil || row.State != "lost" {
		t.Fatalf("lost running call: %+v %v", row, err)
	}
	if row.ErrorCode == nil || *row.ErrorCode != "executor_lost" {
		t.Fatalf("missing lost code: %+v", row)
	}
}

func TestSandboxToolCallUserCancelBeforeCommitIsCancelled(t *testing.T) {
	now := time.Now().UTC()
	lease := now.Add(time.Second)
	row := sandboxToolCallRow{State: "dispatched", DeadlineAt: now.Add(time.Minute), LeaseExpiresAt: &lease}
	stopSandboxToolCall(&row, "user", now)
	if row.State != "cancelled" || row.LeaseExpiresAt != nil || row.FinishedAt == nil {
		t.Fatalf("dispatched cancel: %+v", row)
	}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "started"}, now); err != nil || row.State != "cancelled" {
		t.Fatalf("start after cancel: %+v %v", row, err)
	}
}

func TestSandboxToolCallInterruptedMapsReasonIndependentOfServerState(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		state, reason, want string
		code                string
	}{
		{"running", "deadline", "cancelled", ""},
		{"running", "stop_requested", "cancelled", ""},
		{"running", "executor_shutdown", "failed", "executor_interrupted"},
		{"dispatched", "executor_shutdown", "failed", "executor_interrupted"},
		{"cancel_requested", "executor_shutdown", "cancelled", ""},
		{"cancel_requested", "stop_requested", "cancelled", ""},
	} {
		lease := now.Add(time.Second)
		row := sandboxToolCallRow{State: tc.state, DeadlineAt: now.Add(time.Minute), LeaseExpiresAt: &lease}
		err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "interrupted", Reason: tc.reason, Result: []byte(`{"content":[]}`)}, now)
		if err != nil || row.State != tc.want || row.FinishedAt == nil || string(row.Result) != `{"content":[]}` {
			t.Fatalf("%s/%s: %+v %v", tc.state, tc.reason, row, err)
		}
		code := ""
		if row.ErrorCode != nil {
			code = *row.ErrorCode
		}
		if code != tc.code {
			t.Fatalf("%s/%s code=%q", tc.state, tc.reason, code)
		}
	}
}

func TestSandboxToolCallResultConflictAndReplay(t *testing.T) {
	now := time.Now().UTC()
	digest := "digest"
	size := int64(4)
	row := sandboxToolCallRow{State: "succeeded", ResultSHA256: &digest, ResultBytes: &size}
	other := "different"
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "succeeded", ResultSHA256: &other, ResultBytes: &size}, now); !errors.Is(err, ErrToolCallConflict) {
		t.Fatalf("changed result: %v", err)
	}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "succeeded", ResultSHA256: &digest, ResultBytes: &size}, now); err != nil {
		t.Fatal(err)
	}
}

func TestSandboxToolCallCompletedResultWinsPendingStop(t *testing.T) {
	now := time.Now().UTC()
	row := sandboxToolCallRow{State: "cancel_requested", DeadlineAt: now.Add(time.Minute)}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "succeeded", Result: []byte(`{"content":[]}`)}, now); err != nil || row.State != "succeeded" {
		t.Fatalf("result lost: %+v %v", row, err)
	}
	stopSandboxToolCall(&row, "user", now)
	if row.State != "succeeded" {
		t.Fatal("cancel replaced completed result")
	}
}

func TestSandboxToolCallFinalizerPreservesAcceptedResult(t *testing.T) {
	now := time.Now().UTC()
	name := "memory_version"
	row := sandboxToolCallRow{State: "running", DeadlineAt: now.Add(time.Minute), Finalizer: &name, FinalizeTimeoutMS: 30000}
	if err := applySandboxToolCallReport(&row, SandboxToolCallReport{Event: "succeeded", Result: []byte(`{"content":[]}`)}, now); err != nil || row.State != "finalizing" || row.FinalizeUntil == nil {
		t.Fatalf("finalizer skipped: %+v %v", row, err)
	}
	stopSandboxToolCall(&row, "user", now)
	if row.State != "finalizing" {
		t.Fatal("cancel discarded accepted result")
	}
}

func TestSandboxToolCallClaimSQLAndBindings(t *testing.T) {
	scope := SandboxToolCallScope{OrganizationUUID: "org", WorkspaceUUID: "workspace"}
	now := time.Date(2026, time.November, 13, 0, 0, 0, 0, time.UTC)
	assertMapperBuilderContract(t, mapperBuilderContract{statement: sandboxToolCallMapperClaimStatement, bound: buildSandboxToolCallMapperClaim(yourbatis.DialectPostgres, scope, "session", now, 3), wantID: "SandboxToolCallMapper.Claim", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "codeSessionUUID", "now", "limit"}, wantSQLFragments: []string{"organization_uuid = $1", "workspace_uuid = $2", "state = 'pending'", "deadline_at > $4", "ORDER BY id", "FOR UPDATE SKIP LOCKED"}})
	assertMapperBuilderContract(t, mapperBuilderContract{statement: sandboxToolCallMapperClaimDueStatement, bound: buildSandboxToolCallMapperClaimDue(yourbatis.DialectPostgres, now, 64), wantID: "SandboxToolCallMapper.ClaimDue", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"now", "now", "now", "now", "now", "now", "limit"}, wantSQLFragments: []string{"state = 'cancel_requested' AND (lease_expires_at + INTERVAL '30 seconds' <= $5 OR stop_grace_until <= $6)", "ORDER BY LEAST(", "FOR UPDATE SKIP LOCKED"}})
	assertMapperBuilderContract(t, mapperBuilderContract{statement: sandboxToolCallMapperClaimDueFinalizationsStatement, bound: buildSandboxToolCallMapperClaimDueFinalizations(yourbatis.DialectPostgres, now, 64), wantID: "SandboxToolCallMapper.ClaimDueFinalizations", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"now", "limit"}, wantSQLFragments: []string{"state = 'finalizing' AND finalize_until <= $1", "FOR UPDATE SKIP LOCKED"}})
}

func TestSandboxToolCallParametersPreserveIntegerPrecision(t *testing.T) {
	row := sandboxToolCallRow{ToolName: "read", Input: []byte(`{"start_line":9007199254740992}`)}
	var result SandboxToolCall
	err := toolCallSubmissionResult(row, SandboxToolCallSubmission{Tool: "read", Input: []byte(`{"start_line":9007199254740993}`)}, &result)
	if !errors.Is(err, ErrToolCallConflict) {
		t.Fatalf("different integer inputs collided: %v", err)
	}
}

func TestSandboxToolCallPayloadBindingsAreSensitive(t *testing.T) {
	row := sandboxToolCallRow{Input: []byte(`{"command":"private"}`), Result: []byte(`{"content":[]}`), FinalizerState: []byte(`{}`), LateResult: []byte(`{}`)}
	bound := buildSandboxToolCallMapperInsert(yourbatis.DialectPostgres, row)
	assertMapperBuilderContract(t, mapperBuilderContract{statement: sandboxToolCallMapperInsertStatement, bound: bound, wantID: "SandboxToolCallMapper.Insert", wantKind: yourbatis.StatementInsert,
		wantArgumentNames: []string{"row.UUID", "row.OrganizationUUID", "row.WorkspaceUUID", "row.CodeSessionUUID", "row.ExternalID", "row.ThreadID", "row.WorkerEpoch", "row.SandboxUUID", "row.Attempt", "row.ToolName", "row.Input", "row.State", "row.DeadlineAt", "row.TimeoutMS", "row.LeaseExpiresAt", "row.CancelRequestedAt", "row.CancelReason", "row.StopGraceUntil", "row.Result", "row.ResultObjectKey", "row.ResultBytes", "row.ResultSHA256", "row.Finalizer", "row.FinalizerState", "row.FinalizeTimeoutMS", "row.FinalizeUntil", "row.ErrorCode", "row.LateResult", "row.CreatedAt", "row.UpdatedAt", "row.FinishedAt"}, wantSensitiveArgumentNames: []string{"row.Input", "row.Result", "row.FinalizerState", "row.LateResult"}, wantSQLFragments: []string{"INSERT INTO sandbox_tool_calls", "ON CONFLICT (code_session_uuid, thread_id, external_id) DO NOTHING"}})
}

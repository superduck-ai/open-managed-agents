package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestHarnessCreateTurnAndOrderBindings(t *testing.T) {
	ref := HarnessTurnRef{UUID: "input", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}
	create := buildCloudHarnessMapperCreateForInput(yourbatis.DialectPostgres, ref)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperCreateForInputStatement, bound: create,
		wantID: "CloudHarnessMapper.CreateForInput", wantKind: yourbatis.StatementInsert,
		wantArgumentNames: []string{"ref.UUID", "ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID"},
		wantSQLFragments:  []string{"INSERT INTO harness_thread_state", "e.processed_at IS NULL", "state.execution_input_uuid IS DISTINCT FROM EXCLUDED.execution_input_uuid"},
	})
	earlier := buildCloudHarnessMapperHasEarlierPendingInput(yourbatis.DialectPostgres, ref, ref.UUID)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperHasEarlierPendingInputStatement, bound: earlier,
		wantID: "CloudHarnessMapper.HasEarlierPendingInput", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"inputUUID", "ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID"},
		wantSQLFragments:  []string{"earlier.id < current.id", "earlier.processed_at IS NULL", "current.uuid = $1"},
	})
	want := []any{"input", "org", "workspace", "session"}
	if !reflect.DeepEqual(create.Values(), want) || !reflect.DeepEqual(earlier.Values(), want) {
		t.Fatal("input scope parameters are out of order")
	}
}

func TestHarnessModelLeaseBindings(t *testing.T) {
	ref := HarnessTurnRef{UUID: "turn", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}
	until := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	lease := HarnessTurnLease{Ref: ref, CodeSessionUUID: "code", Epoch: 17}
	lock := buildCloudHarnessMapperLockModelLease(yourbatis.DialectPostgres, lease)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperLockModelLeaseStatement, bound: lock,
		wantID: "CloudHarnessMapper.LockModelLease", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"lease.CodeSessionUUID", "lease.Ref.OrganizationUUID", "lease.Ref.WorkspaceUUID", "lease.Ref.SessionUUID", "lease.Epoch"},
		wantSQLFragments:  []string{"workspace_uuid = $3", "current_worker_epoch = $5", "worker_lease_expires_at > NOW()", "FOR UPDATE"},
	})
	if want := []any{"code", "org", "workspace", "session", int64(17)}; !reflect.DeepEqual(lock.Values(), want) {
		t.Fatalf("lock parameters = %#v, want %#v", lock.Values(), want)
	}
	grant := buildCloudHarnessMapperGrantModelLease(yourbatis.DialectPostgres, harnessClaimParams{
		Ref: ref, CodeSessionUUID: "code", TokenHash: "test hash", LeaseUntil: until,
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperGrantModelLeaseStatement, bound: grant,
		wantID: "CloudHarnessMapper.GrantModelLease", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames:          []string{"params.TokenHash", "params.LeaseUntil", "params.CodeSessionUUID", "params.Ref.OrganizationUUID", "params.Ref.WorkspaceUUID", "params.Ref.SessionUUID"},
		wantSensitiveArgumentNames: []string{"params.TokenHash"},
		wantSQLFragments:           []string{"worker_lease_expires_at <= NOW()", "RETURNING current_worker_epoch"},
	})
	if want := []any{"test hash", until, "code", "org", "workspace", "session"}; !reflect.DeepEqual(grant.Values(), want) {
		t.Fatal("grant parameters are out of order")
	}
}

func TestHarnessToolCheckpointBindings(t *testing.T) {
	ref := HarnessTurnRef{UUID: "turn", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}
	params := harnessToolRecord{
		Key: harnessThreadKey{Scope: ref.conversationScope(), UUID: "thread"}, MessageID: "assistant", TurnUUID: ref.UUID, ToolUUID: "tool",
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperAttachToolStatement,
		bound:     buildCloudHarnessMapperAttachTool(yourbatis.DialectPostgres, params),
		wantID:    "CloudHarnessMapper.AttachTool", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"params.TurnUUID", "params.ToolUUID", "params.Key.Scope.OrganizationUUID", "params.Key.Scope.WorkspaceUUID",
			"params.Key.Scope.SessionUUID", "params.Key.UUID", "params.MessageID"},
		wantSQLFragments: []string{"UPDATE harness_conversation_history", "execution_turn_uuid = $1", "message_id = $7", "tool_uuid IS NULL"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperFinishToolStatement,
		bound:     buildCloudHarnessMapperFinishTool(yourbatis.DialectPostgres, harnessToolResultParams{Ref: ref, ToolUUID: "tool", Text: "output"}),
		wantID:    "CloudHarnessMapper.FinishTool", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"params.Text", "params.Failed", "params.Ref.OrganizationUUID", "params.Ref.WorkspaceUUID",
			"params.Ref.SessionUUID", "params.Ref.UUID", "params.ToolUUID"},
		wantSensitiveArgumentNames: []string{"params.Text"},
		wantSQLFragments:           []string{"tool_result_text = $1", "execution_turn_uuid = $6", "tool_uuid = $7", "tool_result_text IS NULL"},
	})
}

func TestHarnessChoiceBindings(t *testing.T) {
	ref := HarnessTurnRef{UUID: "turn", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}
	args := []string{"ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID"}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperLockWaitingChoiceStatement,
		bound:     buildCloudHarnessMapperLockWaitingChoice(yourbatis.DialectPostgres, ref),
		wantID:    "CloudHarnessMapper.LockWaitingChoice", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: args, wantSQLFragments: []string{"t.execution->>'state' = 'waiting_choice'", "FOR UPDATE OF t"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperFindResumeWorkStatement,
		bound:     buildCloudHarnessMapperFindResumeWork(yourbatis.DialectPostgres, ref, HarnessChoiceResponse),
		wantID:    "CloudHarnessMapper.FindResumeWork", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: append(args, "ref.UUID", "kind"), wantSQLFragments: []string{"t.execution->>'resume_input_uuid' = $4", "t.execution->>'resume_input_kind' = $5"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperResumeChoiceStatement,
		bound:     buildCloudHarnessMapperResumeChoice(yourbatis.DialectPostgres, ref, "answer"),
		wantID:    "CloudHarnessMapper.ResumeChoice", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"taskUUID", "taskUUID", "ref.UUID", "ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID"},
		wantSQLFragments:  []string{"'task_uuid', CAST($1 AS text)", "t.execution->>'state' = 'waiting_choice'"},
	})
}

func TestHarnessExternalWakeBindings(t *testing.T) {
	ref := HarnessTurnRef{UUID: "turn", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}
	params := harnessLeaseParams{Lease: HarnessTurnLease{Ref: ref, Epoch: 17}}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperParkToolStatement, bound: buildCloudHarnessMapperParkTool(yourbatis.DialectPostgres, params, "tool"),
		wantID: "CloudHarnessMapper.ParkTool", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"toolUUID", "params.Lease.Ref.UUID", "params.Lease.Ref.OrganizationUUID", "params.Lease.Ref.WorkspaceUUID",
			"params.Lease.Ref.SessionUUID", "params.Lease.Epoch", "toolUUID"},
		wantSQLFragments: []string{"'state', 'waiting_tool'", "execution->>'tool_uuid' = $7"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperListReadyInputsStatement, bound: buildCloudHarnessMapperListReadyInputs(yourbatis.DialectPostgres, 64),
		wantID: "CloudHarnessMapper.ListReadyInputs", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"limit"},
		wantSQLFragments:  []string{"call.organization_uuid = t.organization_uuid", "call.workspace_uuid = t.workspace_uuid", "LIMIT $1"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperMarkInputPublishedStatement, bound: buildCloudHarnessMapperMarkInputPublished(yourbatis.DialectPostgres, ref, "input"),
		wantID: "CloudHarnessMapper.MarkInputPublished", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"ref.UUID", "ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID", "inputUUID"},
		wantSQLFragments:  []string{"execution - 'wake_pending'", "t.execution->>'resume_input_uuid' = $5"},
	})
}

func TestHarnessSubagentTaskBindings(t *testing.T) {
	params := harnessTaskParams{
		Lease: HarnessTurnLease{Ref: HarnessTurnRef{UUID: "turn", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}, Epoch: 17},
		State: "pending", TaskUUID: "task", ToolUUID: "tool", Kind: HarnessSubagentTask,
		SubagentThreadUUID: new("child"), DelegationToolUUID: new("delegate"),
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperAdvanceTaskStatement, bound: buildCloudHarnessMapperAdvanceTask(yourbatis.DialectPostgres, params),
		wantID: "CloudHarnessMapper.AdvanceTask", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"params.State", "params.TaskUUID", "params.ToolUUID", "params.Kind", "params.SubagentThreadUUID", "params.DelegationToolUUID",
			"params.Lease.Ref.UUID", "params.Lease.Ref.OrganizationUUID", "params.Lease.Ref.WorkspaceUUID", "params.Lease.Ref.SessionUUID", "params.Lease.Epoch"},
		wantSQLFragments: []string{"'subagent_thread_uuid', CAST($5 AS text)", "'delegation_tool_uuid', CAST($6 AS text)", "CAST(execution->>'worker_epoch' AS bigint) = $11"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperCountExecutionToolsStatement, bound: buildCloudHarnessMapperCountExecutionTools(yourbatis.DialectPostgres, params.Lease.Ref, params.DelegationToolUUID),
		wantID: "CloudHarnessMapper.CountExecutionTools", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID", "ref.UUID", "delegationToolUUID"},
		wantSQLFragments:  []string{"SELECT COUNT(*)", "turn_uuid = $5"},
	})
}

func TestHarnessInterruptTurnBindings(t *testing.T) {
	ref := HarnessTurnRef{UUID: "turn", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperFindFirstPendingInputStatement,
		bound:     buildCloudHarnessMapperFindFirstPendingInput(yourbatis.DialectPostgres, ref),
		wantID:    "CloudHarnessMapper.FindFirstPendingInput", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID"},
		wantSQLFragments:  []string{"processed_at IS NULL", "ORDER BY id LIMIT 1"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: cloudHarnessMapperInterruptTurnStatement,
		bound:     buildCloudHarnessMapperInterruptTurn(yourbatis.DialectPostgres, harnessInterruptParams{Ref: ref, State: HarnessInterrupted}),
		wantID:    "CloudHarnessMapper.InterruptTurn", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"params.State", "params.State", "params.TaskUUID", "params.ToolUUID", "params.Kind", "params.Ref.UUID", "params.Ref.OrganizationUUID", "params.Ref.WorkspaceUUID", "params.Ref.SessionUUID"},
		wantSQLFragments:  []string{"'lease_until', NULL", "'subagent_thread_uuid', NULL", "execution->>'state' IN ('pending', 'running', 'waiting_choice', 'waiting_tool')"},
	})
}

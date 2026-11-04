package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestDSHWorkerMapperContracts(t *testing.T) {
	scope := DSHScope{OrganizationUUID: "organization", WorkspaceUUID: "workspace", CodeSessionUUID: "code-session", Epoch: 7}
	at := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	dialect := yourbatis.DialectPostgres
	tests := []mapperBuilderContract{
		{statement: dSHWorkerMapperRegisterStatement, bound: buildDSHWorkerMapperRegister(dialect, scope, at), wantID: "DSHWorkerMapper.Register", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"expiresAt", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "scope.Epoch"}, wantSQLFragments: []string{"worker_claim_requested_at = NULL", "current_worker_epoch = $5", "status = 'active'"}, wantSQLAbsent: []string{"worker_turn_started = false", "worker_external_metadata"}},
		{statement: dSHWorkerMapperHeartbeatStatement, bound: buildDSHWorkerMapperHeartbeat(dialect, scope, at), wantID: "DSHWorkerMapper.Heartbeat", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"expiresAt", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "scope.Epoch"}, wantSQLFragments: []string{"worker_last_heartbeat_at = NOW()", "current_worker_epoch = $5"}},
		{statement: dSHWorkerMapperEnsureStateStatement, bound: buildDSHWorkerMapperEnsureState(dialect, scope), wantID: "DSHWorkerMapper.EnsureState", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"ON CONFLICT (organization_uuid, workspace_uuid, code_session_uuid) DO NOTHING"}},
		{statement: dSHWorkerMapperStateStatement, bound: buildDSHWorkerMapperState(dialect, scope), wantID: "DSHWorkerMapper.State", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"CAST(model_control_sequence AS text) AS model_control_sequence, model_control_input_id FROM dsh_worker_states"}},
		{statement: dSHWorkerMapperPatchStatement, bound: buildDSHWorkerMapperPatch(dialect, scope, DSHWorkerPatch{}), wantID: "DSHWorkerMapper.Patch", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"SET updated_at = NOW()"}, wantSQLAbsent: []string{"model =", "trace_turns =", "status ="}},
	}
	for _, test := range tests {
		t.Run(test.wantID, func(t *testing.T) { assertMapperBuilderContract(t, test) })
	}
	model := "selected"
	bound := buildDSHWorkerMapperPatch(dialect, scope, DSHWorkerPatch{ControlInputID: "model-control", ControlSequence: "20", ModelSet: true, Model: &model, TraceTurnsSet: true, TraceTurns: []byte(`[]`)})
	assertMapperBuilderContract(t, mapperBuilderContract{statement: dSHWorkerMapperPatchStatement, bound: bound, wantID: "DSHWorkerMapper.Patch", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"patch.Model", "patch.ControlSequence", "patch.ControlInputID", "patch.TraceTurns", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"model = $1", "model_control_sequence = $2", "model_control_input_id = $3", "trace_turns = $4"}})
	assertMapperBuilderContract(t, mapperBuilderContract{statement: codeSessionClaimMapperLockNextDSHClaimableStatement, bound: buildCodeSessionClaimMapperLockNextDSHClaimable(dialect, 10_000_000), wantID: "CodeSessionClaimMapper.LockNextDSHClaimable", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"leaseGraceMicros"}, wantSQLFragments: []string{"worker_protocol}' = 'dsr1'", "FOR UPDATE OF cs SKIP LOCKED"}, wantSQLAbsent: []string{"worker_protocol}', 'ccrv2') = 'ccrv2'"}})
}

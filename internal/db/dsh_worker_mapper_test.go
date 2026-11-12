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
		{statement: dSHWorkerMapperContextSnapshotStatement, bound: buildDSHWorkerMapperContextSnapshot(dialect, scope), wantID: "DSHWorkerMapper.ContextSnapshot", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"context_snapshot IS NOT NULL"}},
		{statement: dSHWorkerMapperFreezeContextStatement, bound: buildDSHWorkerMapperFreezeContext(dialect, scope, []byte(`{}`)), wantID: "DSHWorkerMapper.FreezeContext", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"snapshot", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSensitiveArgumentNames: []string{"snapshot"}, wantSQLFragments: []string{"context_snapshot IS NULL", "RETURNING clock_timestamp() AS statement_at"}},
		{statement: dSHWorkerMapperRegisterStatement, bound: buildDSHWorkerMapperRegister(dialect, scope, at), wantID: "DSHWorkerMapper.Register", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"expiresAt", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "scope.Epoch"}, wantSQLFragments: []string{"worker_claim_requested_at = NULL", "current_worker_epoch = $5", "status = 'active'"}, wantSQLAbsent: []string{"worker_turn_started = false", "worker_external_metadata"}},
		{statement: dSHWorkerMapperHeartbeatStatement, bound: buildDSHWorkerMapperHeartbeat(dialect, scope, at), wantID: "DSHWorkerMapper.Heartbeat", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"expiresAt", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "scope.Epoch"}, wantSQLFragments: []string{"worker_last_heartbeat_at = NOW()", "current_worker_epoch = $5"}},
		{statement: dSHWorkerMapperEnsureStateStatement, bound: buildDSHWorkerMapperEnsureState(dialect, scope), wantID: "DSHWorkerMapper.EnsureState", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"ON CONFLICT (organization_uuid, workspace_uuid, code_session_uuid) DO NOTHING"}},
	}
	for _, test := range tests {
		t.Run(test.wantID, func(t *testing.T) { assertMapperBuilderContract(t, test) })
	}
	assertMapperBuilderContract(t, mapperBuilderContract{statement: codeSessionClaimMapperLockNextDSHClaimableStatement, bound: buildCodeSessionClaimMapperLockNextDSHClaimable(dialect, 10_000_000), wantID: "CodeSessionClaimMapper.LockNextDSHClaimable", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"leaseGraceMicros"}, wantSQLFragments: []string{"worker_protocol}' = 'dsr1'", "FOR UPDATE OF cs SKIP LOCKED", "runtime_tools}", "sources}", "git_repository", "context.context_snapshot IS NOT NULL", "context.organization_uuid = cs.organization_uuid", "context.workspace_uuid = cs.workspace_uuid", "context.code_session_uuid = cs.uuid"}, wantSQLAbsent: []string{"worker_protocol}', 'ccrv2') = 'ccrv2'"}})
}

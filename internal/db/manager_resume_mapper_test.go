package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestManagerResumeMapperContracts(t *testing.T) {
	target := ManagerResumeTarget{OrganizationUUID: "org", WorkspaceUUID: "workspace", SandboxUUID: "sandbox"}
	contracts := []mapperBuilderContract{
		{statement: managerResumeMapperFindStatement, bound: buildManagerResumeMapperFind(yourbatis.DialectPostgres, "workspace", "provider"), wantID: "ManagerResumeMapper.Find", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"workspaceUUID", "providerID"}, wantSQLFragments: []string{"sandbox.workspace_uuid=$1", "sandbox.provider_sandbox_id=$2", "sandbox.state='running'", "s.organization_uuid=work.organization_uuid", "code.workspace_uuid=s.workspace_uuid", "s.archived_at IS NULL"}},
		{statement: managerResumeMapperTryLockStatement, bound: buildManagerResumeMapperTryLock(yourbatis.DialectPostgres, "sandbox"), wantID: "ManagerResumeMapper.TryLock", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"sandboxUUID"}, wantSQLFragments: []string{"pg_try_advisory_xact_lock(hashtextextended($1, 476109))"}},
		{statement: managerResumeMapperIsIdleStatement, bound: buildManagerResumeMapperIsIdle(yourbatis.DialectPostgres, target), wantID: "ManagerResumeMapper.IsIdle", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"target.OrganizationUUID", "target.WorkspaceUUID", "target.SandboxUUID"}, wantSQLFragments: []string{"state IN ('dispatched','running','cancel_requested','finalizing')"}},
		{statement: managerResumeMapperRecordVersionStatement, bound: buildManagerResumeMapperRecordVersion(yourbatis.DialectPostgres, target, "0.1.1"), wantID: "ManagerResumeMapper.RecordVersion", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"version", "target.OrganizationUUID", "target.WorkspaceUUID", "target.SandboxUUID"}, wantSQLFragments: []string{"state IN ('creating','running')"}},
	}
	for _, phase := range []string{"checking", "draining", "starting", ""} {
		fragments := []string{"manager_resume_phase=$1", "organization_uuid=$2", "workspace_uuid=$3", "uuid=$4", "state='running'"}
		if phase == "starting" {
			fragments = append(fragments, "tool_call_ready_at=NULL")
		}
		contracts = append(contracts, mapperBuilderContract{statement: managerResumeMapperSetPhaseStatement, bound: buildManagerResumeMapperSetPhase(yourbatis.DialectPostgres, target, phase), wantID: "ManagerResumeMapper.SetPhase", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"phase", "target.OrganizationUUID", "target.WorkspaceUUID", "target.SandboxUUID"}, wantSQLFragments: fragments})
	}
	for _, contract := range contracts {
		assertMapperBuilderContract(t, contract)
	}
}

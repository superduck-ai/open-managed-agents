package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSandboxLifecycleMapperClaimBindsScopeAndIdleCutoff(t *testing.T) {
	cutoff := time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)
	bound := buildSandboxLifecycleMapperClaim(yourbatis.DialectPostgres, "code-session-uuid", "sandbox-uuid", cutoff)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sandboxLifecycleMapperClaimStatement, bound: bound,
		wantID: "SandboxLifecycleMapper.Claim", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"codeSessionUUID", "cutoff", "sandboxUUID"},
		wantSQLFragments: []string{"idle_since = NULL", "current_worker_epoch = current_worker_epoch + 1",
			"worker_status = 'idle'", "idle_since <= $2", "sandbox.uuid = $3", "s.organization_uuid = cs.organization_uuid",
			"e.delivery_status <> 'processed'", "env.config->>'type' = 'cloud'"},
	})
	values := make([]any, len(bound.Args))
	for i, arg := range bound.Args {
		values[i] = arg.Value
	}
	if !reflect.DeepEqual(values, []any{"code-session-uuid", cutoff, "sandbox-uuid"}) {
		t.Fatalf("bound values = %#v", values)
	}
}

func TestSandboxLifecycleMapperDeletionTargetsImmutableTenantSandbox(t *testing.T) {
	scope := sandboxLifecycleScope{OrganizationUUID: "org-uuid", WorkspaceUUID: "workspace-uuid", SandboxUUID: "sandbox-uuid"}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sandboxLifecycleMapperFinishStopStatement,
		bound:     buildSandboxLifecycleMapperFinishStop(yourbatis.DialectPostgres, scope),
		wantID:    "SandboxLifecycleMapper.FinishStop", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.SandboxUUID"},
		wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "uuid = $3", "state = 'stopping'", "stop_reason = 'idle_timeout'"},
	})
}

func TestSandboxLifecycleMapperDisabledSweepStillRepairsCommittedDeletion(t *testing.T) {
	bound := buildSandboxLifecycleMapperListCandidates(yourbatis.DialectPostgres, time.Now(), "sandbox-cursor", 100, false)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sandboxLifecycleMapperListCandidatesStatement, bound: bound,
		wantID: "SandboxLifecycleMapper.ListCandidates", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"after", "limit"},
		wantSQLFragments:  []string{"sandbox.uuid > $1", "sandbox.state = 'stopping'", "LIMIT $2"},
	})
	if containsMapperSQL(bound.SQL, "cs.idle_since") {
		t.Fatal("disabled sweep still selects new idle candidates")
	}
}

func TestSandboxReclamationRecoveryWithoutProviderIDOnlyTargetsReclaimed(t *testing.T) {
	bound := buildEnvironmentSandboxMapperScheduleRecoveryForCodeSession(yourbatis.DialectPostgres, environmentSandboxRecoveryParams{
		CodeSessionExternalID: "code-session", LastError: "reclaimed",
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: environmentSandboxMapperScheduleRecoveryForCodeSessionStatement, bound: bound,
		wantID: "EnvironmentSandboxMapper.ScheduleRecoveryForCodeSession", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames:          []string{"params.CodeSessionExternalID", "params.LastError"},
		wantSensitiveArgumentNames: []string{"params.LastError"},
		wantSQLFragments: []string{"sandbox.state = 'stopped'", "sandbox.stop_reason = 'idle_timeout'",
			"e.delivery_status <> 'processed'", "s.archived_at IS NULL", "code_session.external_id = $1"},
	})
	if containsMapperSQL(bound.SQL, "sandbox.state = 'running'") {
		t.Fatal("empty provider ID allows recovery of a running sandbox")
	}
}

func TestCodeSessionMapperPublicInputResetsIdleClockWithinTenant(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: codeSessionMapperResetIdleSinceForSessionStatement,
		bound:     buildCodeSessionMapperResetIdleSinceForSession(yourbatis.DialectPostgres, "org", "workspace", "session"),
		wantID:    "CodeSessionMapper.ResetIdleSinceForSession", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "sessionUUID"},
		wantSQLFragments:  []string{"idle_since = NULL", "organization_uuid = $1", "workspace_uuid = $2", "session_uuid = $3", "status = 'active'"},
	})
}

package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionUsageMapperBoundScope(t *testing.T) {
	params := sessionUsageTotalsParams{WorkspaceUUID: "workspace", SessionExternalID: "session", ThreadExternalID: "thread"}
	bound := buildSessionUsageMapperSumUsageTotals(yourbatis.DialectPostgres, params)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionUsageMapperSumUsageTotalsStatement, bound: bound,
		wantID: "SessionUsageMapper.SumUsageTotals", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionExternalID", "params.ThreadExternalID"},
		wantSQLFragments:  []string{"e.workspace_uuid = $1", "e.session_external_id = $2", "e.thread_external_id = $3"},
	})
}

func TestSessionUsageActivityUsesScopedThreadStatusUnion(t *testing.T) {
	params := sessionUsageTotalsParams{WorkspaceUUID: "workspace", SessionExternalID: "session", ThreadExternalID: "thread"}
	bound := buildSessionUsageMapperSumActiveSeconds(yourbatis.DialectPostgres, params)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionUsageMapperSumActiveSecondsStatement, bound: bound,
		wantID: "SessionUsageMapper.SumActiveSeconds", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.ThreadExternalID", "params.WorkspaceUUID", "params.SessionExternalID"},
		wantSQLFragments:  []string{"oma_session_activity_metrics(", "NULLIF($1, '')", "s.workspace_uuid = $2", "s.external_id = $3", "s.deleted_at IS NULL"},
	})
}

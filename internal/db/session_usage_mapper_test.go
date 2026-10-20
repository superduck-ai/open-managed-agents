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

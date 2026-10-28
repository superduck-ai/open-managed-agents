package db

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionUsageMapperBoundScope(t *testing.T) {
	params := sessionUsageTotalsParams{WorkspaceUUID: "workspace", SessionExternalID: "session", ThreadExternalID: "thread"}
	bound := buildSessionUsageMapperSumUsageTotals(yourbatis.DialectPostgres, params)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionUsageMapperSumUsageTotalsStatement, bound: bound,
		wantID: "SessionUsageMapper.SumUsageTotals", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionExternalID", "params.ThreadExternalID", "params.ThreadExternalID", "params.WorkspaceUUID", "params.SessionExternalID", "params.ThreadExternalID"},
		wantSQLFragments:  []string{"result.workspace_uuid = $1", "result.session_external_id = $2", "result.thread_external_id = $4", "e.workspace_uuid = $5", "e.session_external_id = $6", "e.thread_external_id = $7", "agent.tool_result", "payload->>'name' = 'web_search'"},
	})
}

func TestSetUsageListCostKeepsRuntime(t *testing.T) {
	raw, err := setUsageCosts([]byte(`{"list_cost":{"amount":"0.003438","currency":"USD"},"runtime_cost_usd":"0.000046"}`), "0.003484", "0.002000", "0.001438")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ListCost struct {
			Amount string `json:"amount"`
		} `json:"list_cost"`
		Model   string `json:"model_cost_usd"`
		Tool    string `json:"tool_cost_usd"`
		Runtime string `json:"runtime_cost_usd"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.ListCost.Amount != "0.003484" || payload.Model != "0.002000" || payload.Tool != "0.001438" || payload.Runtime != "0.000046" {
		t.Fatalf("usage = %s", raw)
	}
}

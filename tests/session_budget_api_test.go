package tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestSessionBudgetUpdateRulesAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("budget-api-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"budget-agent"}`)
	env := createEnvironment(t, app, `{"name":"budget-env"}`)
	created := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)

	postBudget := func(body string) int {
		t.Helper()
		resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+created.ID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// 无预算 session 加预算 → 400（官方 budgets.md 契约）
	if code := postBudget(`{"budget":{"type":"limit","max_list_cost":{"amount":"500","currency":"USD"}}}`); code != http.StatusBadRequest {
		t.Fatalf("add budget to session created without one: status = %d, want 400", code)
	}

	// 创建带预算的 session：改预算 → 200；移除预算（null）→ 200
	budgeted := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`,"budget":{"type":"limit","max_list_cost":{"amount":"125","currency":"USD"}}}`)
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+budgeted.ID+"?beta=true", strings.NewReader(`{"budget":{"type":"limit","max_list_cost":{"amount":"250","currency":"USD"}}}`), defaultTestKey, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update existing budget: status = %d, want 200", resp.StatusCode)
	}
	resp = doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+budgeted.ID+"?beta=true", strings.NewReader(`{"budget":null}`), defaultTestKey, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove budget: status = %d, want 200", resp.StatusCode)
	}
}

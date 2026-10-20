//go:build integration || e2e || e2b_integration

package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSessionBudgetWithoutConfiguredListPrice(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("budget-unpriced"))
	defer app.close()
	agent := createAgent(t, app, fmt.Sprintf(`{"name":"budget-agent-%d","model":{"id":"claude-opus-4-6"},"system":"test"}`, time.Now().UnixNano()))
	env := createEnvironment(t, app, fmt.Sprintf(`{"name":"budget-env-%d"}`, time.Now().UnixNano()))
	body := fmt.Sprintf(`{"agent":%q,"environment_id":%q,"budget":{"type":"limit","max_list_cost":{"amount":"1","currency":"USD"}}}`, agent.ID, env.ID)
	response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions", strings.NewReader(body), defaultTestKey, true)
	defer response.Body.Close()
	data := readAll(t, response.Body)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(data), "no list price configured") {
		t.Fatalf("budget must not be silently ignored: status=%d body=%s", response.StatusCode, data)
	}
	session := createSession(t, app, fmt.Sprintf(`{"agent":%q,"environment_id":%q}`, agent.ID, env.ID))
	response2 := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID, strings.NewReader(`{"budget":{"type":"limit","max_list_cost":{"amount":"1","currency":"USD"}}}`), defaultTestKey, true)
	defer response2.Body.Close()
	if response2.StatusCode != http.StatusBadRequest {
		t.Fatalf("cannot attach a budget to an uncapped session: status=%d body=%s", response2.StatusCode, readAll(t, response2.Body))
	}
}

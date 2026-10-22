//go:build integration

package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/pricing"
)

func TestBrainPricingContextAndBudgetGate(t *testing.T) {
	for _, backend := range []string{llmproviders.Messages, llmproviders.ChatCompletions} {
		t.Run(backend, func(t *testing.T) {
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
			defer upstream.Close()
			app := newPayloadIntegrationApp(t, newFakeStore("pricing-context"))
			worker, _ := newPayloadIntegrationSession(t, app)
			clearTestLLMProviders(t, app)
			provider := seedTestLLMProvider(t, app, "AI 网关", upstream.URL, "server-only-key", "claude-opus-4-6")
			provider.APIBackend = backend
			if _, err := app.db.UpdateLLMProvider(t.Context(), provider); err != nil {
				t.Fatal(err)
			}
			credential := createMessagesCodeSessionCredential(t, app, "claude-opus-4-6")
			registerCodeSessionWorker(t, app, credential.CodeSessionID)
			if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET metadata='{"config":{"worker_transport":"claim"}}' WHERE external_id=$1`, credential.CodeSessionID); err != nil {
				t.Fatal(err)
			}
			session, found, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
			if err != nil || !found {
				t.Fatalf("会话不存在: %v", err)
			}
			session.Budget = json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"100","currency":"USD"}}`)
			save := func() {
				t.Helper()
				if _, _, err := app.db.UpdateSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, session); err != nil {
					t.Fatal(err)
				}
			}
			save()
			path := "/v1/messages"
			if backend == llmproviders.ChatCompletions {
				path = "/v1/chat/completions"
			}
			request := func(code string, status int) {
				t.Helper()
				response := doSessionBearerRequest(t, app, http.MethodPost, path, strings.NewReader(`{"model":"claude-opus-4-6","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`), credential.Token, false)
				defer response.Body.Close()
				body := readAll(t, response.Body)
				if response.StatusCode != status || !strings.Contains(string(body), `"code":"`+code+`"`) || response.Header.Get("X-Should-Retry") != "false" {
					t.Fatalf("门禁响应 %d: %s", response.StatusCode, body)
				}
			}
			request("model_price_unavailable", http.StatusServiceUnavailable)
			model := "claude-opus-4-6"
			price := billing.ModelPrice{Source: "narrows", SourceModel: model, SourceCurrency: "CNY", ExchangeRate: "7.2", Version: "v1", Rates: &billing.Rates{Input: new("7.2"), Output: new("14.4")}}
			if err := app.prices.Replace("narrows", []pricing.CatalogPrice{{Model: model, Price: price, Original: []byte(`{}`)}}); err != nil {
				t.Fatal(err)
			}
			response := doSessionBearerRequest(t, app, http.MethodGet, "/v2/sessions/"+credential.CodeSessionID, nil, codeSessionIngressToken(t, app, credential.CodeSessionID), false)
			var result struct {
				SessionContext struct {
					Billing struct {
						Budget  json.RawMessage                `json:"budget"`
						Pricing map[string]*billing.ModelPrice `json:"pricing"`
					} `json:"billing"`
				} `json:"session_context"`
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("上下文失败: %s", readAll(t, response.Body))
			}
			decodeJSON(t, response.Body, &result)
			response.Body.Close()
			got := result.SessionContext.Billing.Pricing[model]
			if got == nil || got.Source != "narrows" || got.FetchedAt.IsZero() || len(result.SessionContext.Billing.Pricing) != 1 || result.SessionContext.Billing.Budget == nil {
				t.Fatalf("上下文缺少范围内标价: %+v", result)
			}
			session.BudgetReachedAt = new(time.Now())
			save()
			request("session_budget_reached", http.StatusBadRequest)
			if calls.Load() != 0 {
				t.Fatal("预算门禁仍调用上游")
			}
		})
	}
}

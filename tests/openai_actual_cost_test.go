//go:build integration

package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestOpenAIActualCostHTTPAndSessionLedger(t *testing.T) {
	for _, backend := range []string{llmproviders.ChatCompletions, llmproviders.Responses} {
		t.Run(backend, func(t *testing.T) {
			endpoint := "/v1/chat/completions"
			if backend == llmproviders.Responses {
				endpoint = "/v1/responses"
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != endpoint || r.Header.Get("Authorization") != "Bearer server-key" || r.Header.Get("X-Api-Key") != "" || r.Header.Get("Anthropic-Version") != "" {
					t.Errorf("上游鉴权或路径错误：%s", r.URL.Path)
				}
				var request struct {
					Stream bool   `json:"stream"`
					Model  string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				if request.Model != "openai-model" {
					t.Errorf("模型未解析：%s", request.Model)
				}
				body := openAICostFixture(backend, request.Stream)
				w.Header().Set("Content-Type", "application/json")
				if request.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()
			app := newPayloadIntegrationAppWithConfig(t, newFakeStore("openai-cost"), func(c *config.Config) {
				c.Pricing.ResponseCosts = map[string]billing.ResponseCostRule{"default": {AmountPointer: "/gateway/amount", Currency: "CNY", ExchangeRate: "7.2"}}
			})
			clearTestLLMProviders(t, app)
			provider := seedTestLLMProvider(t, app, "OpenAI cost", upstream.URL, "server-key", "openai-model")
			provider.APIBackend = backend
			if _, err := app.db.UpdateLLMProvider(t.Context(), provider); err != nil {
				t.Fatal(err)
			}
			newPayloadIntegrationSessionWithAgent(t, app, `{"name":"openai-cost","model":"openai-model"}`)
			credential := createMessagesCodeSessionCredential(t, app, "openai-model")
			registerCodeSessionWorker(t, app, credential.CodeSessionID)
			if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET metadata='{"config":{"worker_transport":"claim"}}' WHERE external_id=$1`, credential.CodeSessionID); err != nil {
				t.Fatal(err)
			}
			modes := 2
			if backend == llmproviders.ChatCompletions {
				modes = 4
			}
			for mode := range modes {
				stream := mode > 0
				for _, token := range []string{defaultTestKey, credential.Token} {
					field := `"messages":[{"role":"user","content":"hello"}]`
					if backend == llmproviders.Responses {
						field = `"input":"hello"`
					}
					if backend == llmproviders.ChatCompletions && mode >= 2 {
						field += fmt.Sprintf(`,"stream_options":{"include_usage":%t}`, mode == 2)
					}
					input := fmt.Sprintf(`{"model":"openai-model","stream":%t,%s}`, stream, field)
					req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+endpoint, strings.NewReader(input))
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("Content-Type", "application/json")
					response, err := app.client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					body := readAll(t, response.Body)
					_ = response.Body.Close()
					if response.StatusCode != 200 || string(body) != openAICostFixture(backend, stream) {
						t.Fatalf("响应改变：%d/%s", response.StatusCode, body)
					}
				}
			}
			code, found, err := app.db.GetCodeSession(t.Context(), credential.CodeSessionID)
			if err != nil || !found {
				t.Fatal(err)
			}
			_ = requestLifecycleEvents(t, app, code, modes*2)
			var requests, actual int
			var cents int64
			if err := app.pool.QueryRow(t.Context(), `SELECT count(*),count(provider_cost),sum(model_cents) FROM usage_records WHERE model LIKE '%openai-model'`).Scan(&requests, &actual, &cents); err != nil {
				t.Fatal(err)
			}
			if requests != modes*2 || actual != modes*2 || cents != int64(modes*200) {
				t.Fatalf("漏记或重复结算：%d/%d/%d", requests, actual, cents)
			}
			totals, err := app.db.SumSessionUsageTotals(t.Context(), code.WorkspaceUUID, code.SessionExternalID)
			if err != nil {
				t.Fatal(err)
			}
			if totals.ListCostCents != int64(modes*100) {
				t.Fatalf("会话预算未采用实际费用：%+v", totals)
			}
		})
	}
}

func openAICostFixture(backend string, stream bool) string {
	if backend == llmproviders.ChatCompletions {
		usage := `"usage":{"prompt_tokens":1000,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":200}},"gateway":{"amount":"7.2"}`
		if !stream {
			return `{"id":"chat_cost","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],` + usage + `}`
		}
		return "data: {\"id\":\"chat_cost\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"id\":\"chat_cost\",\"choices\":[]," + usage + "}\n\ndata: [DONE]\n\n"
	}
	body := `{"id":"resp_cost","object":"response","status":"completed","output":[{"id":"msg_cost","type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1000,"output_tokens":20,"input_tokens_details":{"cached_tokens":200}},"gateway":{"amount":"7.2"}}`
	if !stream {
		return body
	}
	return "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + body + "}\n\n"
}

package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestLLMProviderAPIBackendConfiguration(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/v1/models" || r.Header.Get("Authorization") != "Bearer server-key" || r.Header.Get("X-Api-Key") != "" || r.Header.Get("Anthropic-Version") != "" {
			t.Errorf("unexpected model discovery request: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"chat-model"}]}`)
	}))
	defer upstream.Close()
	app := newPayloadIntegrationApp(t, newFakeStore("chat-config"))
	cookies := app.platformLoginCookies(t, "chat-config@example.com")
	path := "/api/console/organizations/" + loadDefaultOrganizationUUID(t, app) + "/workspaces/default/llm_providers"
	for _, backend := range []string{"responses", "", "unknown"} {
		body := fmt.Sprintf(`{"name":"invalid","base_url":%q,"api_key":"server-key","api_backend":%q}`, upstream.URL, backend)
		for _, suffix := range []string{"", "/preview_models"} {
			payload := body
			if suffix != "" {
				payload = fmt.Sprintf(`{"base_url":%q,"api_key":"server-key","api_backend":%q}`, upstream.URL, backend)
			}
			response := app.platformRequest(t, http.MethodPost, path+suffix, strings.NewReader(payload), cookies)
			data := readAll(t, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(data), "api_backend_invalid") {
				t.Fatalf("backend=%q status=%d body=%s", backend, response.StatusCode, data)
			}
		}
	}
	body := fmt.Sprintf(`{"name":"Chat","base_url":%q,"api_key":"server-key","api_backend":"chat_completions","model_ids":["chat-model"]}`, upstream.URL+"/prefix")
	created := createConsoleLLMProvider(t, app, path, cookies, body)
	if created["api_backend"] != llmproviders.ChatCompletions {
		t.Fatalf("created=%v", created)
	}
	provider, err := app.db.GetLLMProvider(t.Context(), loadDefaultOrganizationUUID(t, app), getDefaultDBIDs(t, app.pool).WorkspaceUUID, created["id"].(string))
	if err != nil || provider.APIBackend != llmproviders.ChatCompletions {
		t.Fatalf("stored backend=%q err=%v", provider.APIBackend, err)
	}
	preview := app.platformRequest(t, http.MethodPost, path+"/preview_models", strings.NewReader(fmt.Sprintf(`{"base_url":%q,"api_key":"server-key","api_backend":"chat_completions"}`, upstream.URL+"/prefix")), cookies)
	_ = preview.Body.Close()
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("preview=%d", preview.StatusCode)
	}
	update := fmt.Sprintf(`{"name":"Chat renamed","base_url":%q,"model_ids":["chat-model"]}`, upstream.URL+"/prefix")
	response := app.platformRequest(t, http.MethodPut, path+"/"+provider.ExternalID, strings.NewReader(update), cookies)
	defer response.Body.Close()
	var updated map[string]any
	decodeJSON(t, response.Body, &updated)
	if response.StatusCode != http.StatusOK || updated["api_backend"] != llmproviders.ChatCompletions {
		t.Fatalf("update=%v", updated)
	}
	resolved, err := llmproviders.Resolve(t.Context(), app.db, app.vaultSecrets, provider.OrganizationUUID, provider.WorkspaceUUID, "chat-model")
	if err != nil || resolved.APIBackend != llmproviders.ChatCompletions {
		t.Fatalf("resolved backend=%q err=%v", resolved.APIBackend, err)
	}
	defaults, err := app.db.ListLLMProviders(t.Context(), provider.OrganizationUUID, provider.WorkspaceUUID)
	if err != nil || defaults[0].APIBackend != llmproviders.Messages {
		t.Fatalf("legacy backend default missing: %v", err)
	}
}

func TestBrainChatCompletionsProxyLifecycle(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := calls.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer server-key" || r.Header.Get("X-Api-Key") != "" || r.Header.Get("Anthropic-Version") != "" || r.Header.Get("Anthropic-Beta") != "" {
			t.Errorf("unexpected upstream request: %s", r.URL.Path)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Header.Get("X-Chat-Test") == "http_error" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "upstream-chat-request")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chat_%d\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n", index)
		if r.Header.Get("X-Chat-Test") == "incomplete_response" {
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":5,\"prompt_tokens_details\":{\"cached_tokens\":12}}}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	app := newPayloadIntegrationApp(t, newFakeStore("chat-proxy"))
	newPayloadIntegrationSession(t, app)
	provider := seedTestLLMProvider(t, app, "Chat", upstream.URL, "server-key", "chat-model")
	provider.APIBackend = llmproviders.ChatCompletions
	if _, err := app.db.UpdateLLMProvider(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	credential := createMessagesCodeSessionCredential(t, app, "chat-model")
	body := `{"model":"chat-model","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	request := func(token, mode string) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v1/chat/completions", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		req.Header.Set("Anthropic-Beta", "test-beta")
		req.Header.Set("X-Chat-Test", mode)
		res, err := app.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, token := range []string{defaultTestKey, credential.Token} {
		response := request(token, "")
		_ = response.Body.Close()
		if response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unexpected credential status=%d", response.StatusCode)
		}
	}
	registerCodeSessionWorker(t, app, credential.CodeSessionID)
	response := request(credential.Token, "")
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("sandbox status=%d", response.StatusCode)
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET metadata='{"config":{"worker_transport":"claim"}}' WHERE external_id=$1`, credential.CodeSessionID); err != nil {
		t.Fatal(err)
	}
	response = doMessagesRequest(t, app, credential.Token, body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatalf("backend mismatch status=%d upstream calls=%d", response.StatusCode, calls.Load())
	}
	token := codeSessionIngressToken(t, app, credential.CodeSessionID)
	response = doSessionBearerRequest(t, app, http.MethodGet, "/v2/sessions/"+credential.CodeSessionID, nil, token, false)
	var context struct {
		SessionContext struct {
			Backends map[string]string `json:"model_backends"`
		} `json:"session_context"`
	}
	decodeJSON(t, response.Body, &context)
	_ = response.Body.Close()
	if context.SessionContext.Backends["chat-model"] != llmproviders.ChatCompletions || context.SessionContext.Backends["claude-opus-4-6"] != llmproviders.Messages {
		t.Fatalf("backends=%v", context.SessionContext.Backends)
	}
	code, found, err := app.db.GetCodeSession(t.Context(), credential.CodeSessionID)
	if err != nil || !found {
		t.Fatalf("code session=%t err=%v", found, err)
	}
	for index, mode := range []string{"http_error", "incomplete_response", "success"} {
		response = request(credential.Token, mode)
		data := readAll(t, response.Body)
		_ = response.Body.Close()
		if mode == "http_error" {
			if response.StatusCode != http.StatusForbidden || response.Header.Get("X-Should-Retry") != "false" {
				t.Fatalf("auth status=%d", response.StatusCode)
			}
		} else if response.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.StatusCode, data)
		}
		events := requestLifecycleEvents(t, app, code, (index+1)*2)
		var end struct {
			StartID    string `json:"model_request_start_id"`
			UpstreamID string `json:"upstream_request_id"`
			Error      *struct {
				Type string `json:"type"`
			} `json:"error"`
			Usage struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
				Cache  int `json:"cache_read_input_tokens"`
			} `json:"model_usage"`
		}
		if err := json.Unmarshal(events[len(events)-1].Payload, &end); err != nil {
			t.Fatal(err)
		}
		if end.StartID != response.Header.Get("Request-Id") {
			t.Fatalf("request ID=%s span=%s", response.Header.Get("Request-Id"), end.StartID)
		}
		if mode != "success" {
			expected := mode
			if mode == "http_error" {
				expected = "http_error"
			}
			if end.Error == nil || end.Error.Type != expected {
				t.Fatalf("mode=%s error=%+v", mode, end.Error)
			}
		} else {
			if end.Error != nil || end.Usage.Input != 8 || end.Usage.Output != 5 || end.Usage.Cache != 12 || end.UpstreamID != "upstream-chat-request" {
				t.Fatalf("end=%+v", end)
			}
			history := listSessionEvents(t, app, code.SessionExternalID, "order=asc&limit=100", defaultTestKey)
			expected := maevents.StableAssistantEventID(code.ExternalID, "chat_3", 0, "agent.message")
			count := 0
			for _, event := range history.Data {
				if sessionEventStringField(t, event, "id") == expected {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("stable assistant count=%d", count)
			}
		}
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET worker_lease_expires_at=now()-interval '1 second' WHERE external_id=$1`, code.ExternalID); err != nil {
		t.Fatal(err)
	}
	response = request(credential.Token, "")
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || calls.Load() != 3 {
		t.Fatalf("expired lease status=%d calls=%d", response.StatusCode, calls.Load())
	}
}

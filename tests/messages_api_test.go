package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const messagesTestModel = "claude-opus-4-8"

func TestMessagesAPIFailures(t *testing.T) {
	t.Run("failure upstream key is required", func(t *testing.T) {
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		cfg.AnthropicUpstream.APIKey = ""
		app := newTestAppWithStore(t, &cfg, newFakeStore("messages-no-upstream-key-bucket"))
		defer app.close()

		resp := doMessagesRequest(t, app, defaultTestKey, `{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusServiceUnavailable, "api_error")
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_unexpected"}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AnthropicUpstream.BaseURL = upstream.URL
	cfg.AnthropicUpstream.APIKey = "sk-ant-messages-failure-upstream"
	app := newTestAppWithStore(t, &cfg, newFakeStore("messages-failures-bucket"))
	defer app.close()

	t.Run("failure credential issue rejects mismatched tenant", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		_, err := app.db.GetCodeSessionCredentialContextForIssue(
			context.Background(),
			uuid.NewString(),
			credential.WorkspaceUUID,
			credential.CodeSessionID,
		)
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("issue credential with mismatched organization error = %v, want ErrNotFound", err)
		}
		_, err = app.db.GetCodeSessionCredentialContextForIssue(
			context.Background(),
			credential.OrganizationUUID,
			uuid.NewString(),
			credential.CodeSessionID,
		)
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("issue credential with mismatched workspace error = %v, want ErrNotFound", err)
		}
	})

	t.Run("failure session credential cannot access other resources", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		req, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/models", nil)
		if err != nil {
			t.Fatalf("new models request: %v", err)
		}
		req.Header.Set("X-Api-Key", credential.Token)
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatalf("do models request: %v", err)
		}
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure unregistered credential is rejected but can register", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		resp := doMessagesRequest(t, app, credential.Token, `{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
		if epoch := registerCodeSessionWorker(t, app, credential.CodeSessionID); epoch != "1" {
			t.Fatalf("initial worker epoch = %q, want 1", epoch)
		}
	})

	t.Run("failure expired worker lease rejects credential", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		registerCodeSessionWorker(t, app, credential.CodeSessionID)
		if _, err := app.pool.Exec(context.Background(), `
			update code_sessions
			set worker_lease_expires_at = now() - interval '1 minute'
			where external_id = $1
		`, credential.CodeSessionID); err != nil {
			t.Fatalf("expire Messages credential worker lease: %v", err)
		}
		resp := doMessagesRequest(t, app, credential.Token, `{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure terminated public session rejects credential", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		registerCodeSessionWorker(t, app, credential.CodeSessionID)
		var previousStatus string
		if err := app.pool.QueryRow(context.Background(), `select status from sessions where uuid = $1`, credential.PublicSessionUUID).Scan(&previousStatus); err != nil {
			t.Fatalf("load public session status: %v", err)
		}
		t.Cleanup(func() {
			_, _ = app.pool.Exec(context.Background(), `update sessions set status = $2 where uuid = $1`, credential.PublicSessionUUID, previousStatus)
		})
		if _, err := app.pool.Exec(context.Background(), `update sessions set status = 'terminated' where uuid = $1`, credential.PublicSessionUUID); err != nil {
			t.Fatalf("terminate public session: %v", err)
		}
		resp := doMessagesRequest(t, app, credential.Token, `{"model":"`+messagesTestModel+`","max_tokens":16,"messages":[]}`)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure removed bridge endpoint rejects session credential", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		resp := doSessionBearerRequest(t, app, http.MethodPost, "/v1/code/sessions/"+credential.CodeSessionID+"/bridge", strings.NewReader(`{}`), credential.Token, false)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure removed bridge endpoint is not found for workspace credential", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		resp := doSessionBearerRequest(t, app, http.MethodPost, "/v1/code/sessions/"+credential.CodeSessionID+"/bridge", strings.NewReader(`{}`), defaultTestKey, false)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})
}

func TestMessagesAPISuccess(t *testing.T) {
	type observedRequest struct {
		Path             string
		Query            string
		APIKey           string
		Authorization    string
		Cookie           string
		OrganizationUUID string
		WorkspaceID      string
		AnthropicVersion string
		Body             string
	}
	var mu sync.Mutex
	var observed []observedRequest
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		observed = append(observed, observedRequest{
			Path:             r.URL.Path,
			Query:            r.URL.RawQuery,
			APIKey:           r.Header.Get("X-Api-Key"),
			Authorization:    r.Header.Get("Authorization"),
			Cookie:           r.Header.Get("Cookie"),
			OrganizationUUID: r.Header.Get("X-Organization-UUID"),
			WorkspaceID:      r.Header.Get("X-Workspace-ID"),
			AnthropicVersion: r.Header.Get("Anthropic-Version"),
			Body:             string(body),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Anthropic-Ratelimit-Requests-Remaining", "42")
		_, _ = w.Write([]byte(`{"id":"msg_messages_test","type":"message"}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AnthropicUpstream.BaseURL = upstream.URL
	cfg.AnthropicUpstream.APIKey = "sk-ant-messages-upstream"
	app := newTestAppWithStore(t, &cfg, newFakeStore("messages-success-bucket"))
	defer app.close()
	payload := `{"model":"` + messagesTestModel + `","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
	codeSessionPayload := `{"model":"claude-sonnet-4-6","max_tokens":16,"messages":[]}`

	t.Run("success API credential uses canonical endpoint", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages?beta=true", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("new messages request: %v", err)
		}
		req.Header.Set("X-Api-Key", defaultTestKey)
		req.Header.Set("Authorization", "Bearer must-not-forward")
		req.Header.Set("Cookie", "sessionKey=must-not-forward")
		req.Header.Set("X-Organization-UUID", "must-not-forward")
		req.Header.Set("X-Workspace-ID", "must-not-forward")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatalf("do messages request: %v", err)
		}
		defer resp.Body.Close()
		assertMessagesResponse(t, resp)
	})

	t.Run("success code session credential forwards requested model", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		workerEpoch := registerCodeSessionWorker(t, app, credential.CodeSessionID)
		if _, err := app.pool.Exec(context.Background(), `
			update code_sessions set created_at = now() - interval '30 days' where external_id = $1
		`, credential.CodeSessionID); err != nil {
			t.Fatalf("age lifecycle-bound Messages credential: %v", err)
		}
		resp := doMessagesRequest(t, app, credential.Token, codeSessionPayload)
		defer resp.Body.Close()
		assertMessagesResponse(t, resp)

		assertCodeSessionWorkerHeartbeat(t, app, credential.CodeSessionID, workerEpoch)
		workerResp := doMessagesRequest(t, app, credential.Token, codeSessionPayload)
		defer workerResp.Body.Close()
		assertMessagesResponse(t, workerResp)

		jwtResp := doMessagesRequest(t, app, codeSessionIngressToken(t, app, credential.CodeSessionID), codeSessionPayload)
		assertError(t, jwtResp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("success platform session uses canonical endpoint", func(t *testing.T) {
		cookies := app.platformLoginCookies(t, "messages-canonical@example.com")
		req, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("new platform messages request: %v", err)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatalf("do platform messages request: %v", err)
		}
		defer resp.Body.Close()
		assertMessagesResponse(t, resp)
	})

	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 4 {
		t.Fatalf("upstream requests = %#v, want 4", observed)
	}
	first := observed[0]
	if first.Path != "/v1/messages" || first.Query != "beta=true" || first.APIKey != cfg.AnthropicUpstream.APIKey {
		t.Fatalf("unexpected upstream target or credential: %#v", first)
	}
	if first.Authorization != "" || first.Cookie != "" || first.OrganizationUUID != "" || first.WorkspaceID != "" {
		t.Fatalf("sensitive request headers reached upstream: %#v", first)
	}
	if first.AnthropicVersion != "2023-06-01" || first.Body != payload {
		t.Fatalf("request contract was not preserved: %#v", first)
	}
	if observed[1].Body != codeSessionPayload || observed[2].Body != codeSessionPayload {
		t.Fatalf("code session request bodies = %q, %q; want %q", observed[1].Body, observed[2].Body, codeSessionPayload)
	}
	for _, request := range observed {
		if request.APIKey != cfg.AnthropicUpstream.APIKey {
			t.Fatalf("upstream API key = %q, want configured key", request.APIKey)
		}
	}
}

func TestMessagesWebSearchGateway(t *testing.T) {
	tavily := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode search request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if request["query"] != "latest Go release" || r.Header.Get("Authorization") != "Bearer tavily-test-key" {
			t.Errorf("search request = %#v", request)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"title":"Go release notes","url":"https://go.dev/doc/devel/release","content":"release details"}]}`)
	}))
	defer tavily.Close()

	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := upstreamCalls.Add(1)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode BYOK request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		encoded, _ := json.Marshal(request)
		if strings.Contains(string(encoded), "tavily-test-key") {
			t.Error("search key reached BYOK")
			http.Error(w, "secret reached upstream", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if requestNumber == 1 {
			tools := request["tools"].([]any)
			tool := tools[0].(map[string]any)
			if tool["name"] != "oma_web_search" {
				t.Errorf("projected tool = %#v", tool)
				return
			}
			if _, ok := tool["type"]; ok {
				t.Errorf("projected tool has type: %#v", tool)
				return
			}
			_, _ = io.WriteString(w, `{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"latest Go release"}}],"stop_reason":"tool_use"}`)
			return
		}
		requestMessages := request["messages"].([]any)
		if len(requestMessages) != 3 {
			t.Errorf("continuation messages = %#v", requestMessages)
			return
		}
		_, _ = io.WriteString(w, `{"id":"msg_final","type":"message","content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AnthropicUpstream.BaseURL = upstream.URL
	cfg.AnthropicUpstream.APIKey = "messages-gateway-upstream"
	cfg.WebSearch.Provider = "tavily"
	cfg.WebSearch.Providers["tavily"] = config.WebSearchProviderConfig{Endpoint: tavily.URL, APIKey: "tavily-test-key"}
	app := newTestAppWithStore(t, &cfg, newFakeStore("messages-web-search-gateway-bucket"))
	defer app.close()

	credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
	registerCodeSessionWorker(t, app, credential.CodeSessionID)
	payload := `{"model":"` + messagesTestModel + `","max_tokens":16,"messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`
	response := doMessagesRequest(t, app, credential.Token, payload)
	defer response.Body.Close()
	var body map[string]any
	decodeJSON(t, response.Body, &body)
	content := body["content"].([]any)
	if content[0].(map[string]any)["type"] != "server_tool_use" || content[1].(map[string]any)["type"] != "web_search_tool_result" {
		t.Fatalf("gateway response content = %#v", content)
	}
	if upstreamCalls.Load() != 2 {
		t.Fatalf("BYOK calls = %d, want 2", upstreamCalls.Load())
	}
}

func TestMessagesWebSearchGatewayMixedToolContinuation(t *testing.T) {
	var searchCalls atomic.Int64
	tavily := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchCalls.Add(1)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode search request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if request["query"] != "latest Go release" || r.Header.Get("Authorization") != "Bearer tavily-mixed-key" {
			t.Errorf("search request = %#v", request)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"title":"Go release notes","url":"https://go.dev/doc/devel/release","content":"release details"}]}`)
	}))
	defer tavily.Close()

	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := upstreamCalls.Add(1)
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read BYOK request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if strings.Contains(string(requestBody), "tavily-mixed-key") {
			t.Error("search key reached BYOK")
			http.Error(w, "secret reached upstream", http.StatusBadRequest)
			return
		}
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(requestBody, &request); err != nil {
			t.Errorf("decode BYOK request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if requestNumber == 1 {
			_, _ = io.WriteString(w, `{"id":"msg_mixed","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_search","name":"oma_web_search","input":{"query":"latest Go release"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"go version"}}],"stop_reason":"tool_use"}`)
			return
		}
		if len(request.Messages) != 3 {
			t.Errorf("continuation messages = %d, want 3", len(request.Messages))
			return
		}
		var assistantContent []json.RawMessage
		if err := json.Unmarshal(request.Messages[1].Content, &assistantContent); err != nil {
			t.Errorf("decode projected assistant content: %v", err)
			return
		}
		if request.Messages[1].Role != "assistant" || len(assistantContent) != 2 ||
			!strings.Contains(string(assistantContent[0]), `"id":"toolu_search"`) ||
			!strings.Contains(string(assistantContent[1]), `"id":"toolu_bash"`) ||
			strings.Contains(string(request.Messages[1].Content), "server_tool_use") {
			t.Errorf("projected assistant content = %s", request.Messages[1].Content)
			return
		}
		var resultContent []json.RawMessage
		if err := json.Unmarshal(request.Messages[2].Content, &resultContent); err != nil {
			t.Errorf("decode merged tool results: %v", err)
			return
		}
		if request.Messages[2].Role != "user" || len(resultContent) != 2 ||
			!strings.Contains(string(resultContent[0]), `"tool_use_id":"toolu_search"`) ||
			!strings.Contains(string(resultContent[1]), `"tool_use_id":"toolu_bash"`) {
			t.Errorf("merged tool results = %s", request.Messages[2].Content)
			return
		}
		_, _ = io.WriteString(w, `{"id":"msg_mixed_final","type":"message","role":"assistant","content":[{"type":"text","text":"Go is installed and the release notes are current."}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AnthropicUpstream.BaseURL = upstream.URL
	cfg.AnthropicUpstream.APIKey = "messages-mixed-gateway-upstream"
	cfg.WebSearch.Provider = "tavily"
	cfg.WebSearch.Providers["tavily"] = config.WebSearchProviderConfig{Endpoint: tavily.URL, APIKey: "tavily-mixed-key"}
	app := newTestAppWithStore(t, &cfg, newFakeStore("messages-mixed-web-search-gateway-bucket"))
	defer app.close()

	credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
	registerCodeSessionWorker(t, app, credential.CodeSessionID)
	tools := []map[string]any{
		{"type": "web_search_20250305", "name": "web_search", "max_uses": 2},
		{"name": "bash", "description": "Run a shell command", "input_schema": map[string]any{"type": "object"}},
	}
	firstPayload, err := json.Marshal(map[string]any{
		"model":      messagesTestModel,
		"max_tokens": 16,
		"messages":   []map[string]any{{"role": "user", "content": "search and inspect"}},
		"tools":      tools,
	})
	if err != nil {
		t.Fatalf("encode first mixed request: %v", err)
	}
	firstResponse := doMessagesRequest(t, app, credential.Token, string(firstPayload))
	defer firstResponse.Body.Close()
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("first mixed response status = %d", firstResponse.StatusCode)
	}
	var firstBody struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
	}
	decodeJSON(t, firstResponse.Body, &firstBody)
	if firstBody.StopReason != "tool_use" || len(firstBody.Content) != 2 {
		t.Fatalf("first mixed response = %#v", firstBody)
	}
	var searchUse struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(firstBody.Content[0], &searchUse); err != nil {
		t.Fatalf("decode pending search use: %v", err)
	}
	if searchUse.Type != "server_tool_use" || !strings.HasPrefix(searchUse.ID, "srvtoolu_") ||
		!strings.Contains(string(firstBody.Content[1]), `"id":"toolu_bash"`) || searchCalls.Load() != 0 || upstreamCalls.Load() != 1 {
		t.Fatalf("first mixed response content = %s, searches = %d, BYOK calls = %d", firstBody.Content, searchCalls.Load(), upstreamCalls.Load())
	}

	continuationMessages := []map[string]any{
		{"role": "user", "content": "search and inspect"},
		{"role": "assistant", "content": firstBody.Content},
		{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": "toolu_bash", "content": "go version go1.25.0"}}},
	}
	invalidPayload, err := json.Marshal(map[string]any{
		"model":      messagesTestModel,
		"max_tokens": 16,
		"messages":   continuationMessages,
		"tools":      tools[1:],
	})
	if err != nil {
		t.Fatalf("encode invalid mixed continuation: %v", err)
	}
	invalidResponse := doMessagesRequest(t, app, credential.Token, string(invalidPayload))
	assertError(t, invalidResponse, http.StatusBadRequest, "invalid_request_error")
	if searchCalls.Load() != 0 || upstreamCalls.Load() != 1 {
		t.Fatalf("invalid continuation searches = %d, BYOK calls = %d; want 0 and 1", searchCalls.Load(), upstreamCalls.Load())
	}

	secondPayload, err := json.Marshal(map[string]any{
		"model":      messagesTestModel,
		"max_tokens": 16,
		"messages":   continuationMessages,
		"tools":      tools,
	})
	if err != nil {
		t.Fatalf("encode mixed continuation: %v", err)
	}
	secondResponse := doMessagesRequest(t, app, credential.Token, string(secondPayload))
	defer secondResponse.Body.Close()
	if secondResponse.StatusCode != http.StatusOK {
		t.Fatalf("second mixed response status = %d", secondResponse.StatusCode)
	}
	var secondBody struct {
		Content []json.RawMessage `json:"content"`
	}
	decodeJSON(t, secondResponse.Body, &secondBody)
	if len(secondBody.Content) != 2 ||
		!strings.Contains(string(secondBody.Content[0]), `"type":"web_search_tool_result"`) ||
		!strings.Contains(string(secondBody.Content[0]), `"tool_use_id":"`+searchUse.ID+`"`) ||
		!strings.Contains(string(secondBody.Content[1]), `"text":"Go is installed`) ||
		searchCalls.Load() != 1 || upstreamCalls.Load() != 2 {
		t.Fatalf("second mixed response content = %s, searches = %d, BYOK calls = %d", secondBody.Content, searchCalls.Load(), upstreamCalls.Load())
	}
}

type messagesCodeSessionCredential struct {
	Token             string
	CodeSessionID     string
	PublicSessionUUID string
	OrganizationUUID  string
	WorkspaceUUID     string
}

func createMessagesCodeSessionCredential(t *testing.T, app *testApp, model string) messagesCodeSessionCredential {
	t.Helper()
	apiKey, err := app.db.GetAPIKey(context.Background(), auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatalf("load default API key: %v", err)
	}
	token, err := ids.New("sk-ant-oat01-test-")
	if err != nil {
		t.Fatalf("generate Messages access token: %v", err)
	}
	codeSessionID, err := ids.New("cse_messages_test_")
	if err != nil {
		t.Fatalf("generate code session ID: %v", err)
	}
	var sessionUUID, sessionExternalID, environmentUUID string
	if err := app.pool.QueryRow(context.Background(), `
		select uuid::text, external_id, environment_uuid::text
		from sessions
		where workspace_uuid = $1 and organization_uuid = $2 and deleted_at is null
		order by uuid
	`, apiKey.WorkspaceUUID, apiKey.OrganizationUUID).Scan(&sessionUUID, &sessionExternalID, &environmentUUID); err != nil {
		t.Fatalf("load Messages credential public session: %v", err)
	}
	now := time.Now().UTC()
	_, err = app.db.CreateCodeSession(context.Background(), db.CreateCodeSessionInput{
		ExternalID:            codeSessionID,
		OrganizationUUID:      apiKey.OrganizationUUID.String(),
		WorkspaceUUID:         apiKey.WorkspaceUUID.String(),
		SessionUUID:           sessionUUID,
		SessionExternalID:     sessionExternalID,
		EnvironmentUUID:       environmentUUID,
		EnvironmentExternalID: "environment_" + codeSessionID,
		PermissionMode:        "bypassPermissions",
		Model:                 model,
		Status:                "active",
		Metadata:              json.RawMessage(`{}`),
		OAuthAccessTokenHash:  auth.HashAPIKey(token),
		CreatedAt:             now,
	})
	if err != nil {
		t.Fatalf("create code session: %v", err)
	}
	return messagesCodeSessionCredential{
		Token:             token,
		CodeSessionID:     codeSessionID,
		PublicSessionUUID: sessionUUID,
		OrganizationUUID:  apiKey.OrganizationUUID.String(),
		WorkspaceUUID:     apiKey.WorkspaceUUID.String(),
	}
}

func doMessagesRequest(t *testing.T, app *testApp, apiKey string, payload string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new messages request: %v", err)
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do messages request: %v", err)
	}
	return resp
}

func assertMessagesResponse(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("messages status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	if remaining := resp.Header.Get("Anthropic-Ratelimit-Requests-Remaining"); remaining != "42" {
		t.Fatalf("rate limit header = %q, want 42", remaining)
	}
	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["id"] != "msg_messages_test" {
		t.Fatalf("messages response = %#v", body)
	}
}

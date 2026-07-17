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
	"testing"
	"time"

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
		cfg.AnthropicUpstreamAPIKey = ""
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
	cfg.AnthropicUpstreamBaseURL = upstream.URL
	cfg.AnthropicUpstreamAPIKey = "sk-ant-messages-failure-upstream"
	app := newTestAppWithStore(t, &cfg, newFakeStore("messages-failures-bucket"))
	defer app.close()

	t.Run("failure credential issue rejects mismatched tenant", func(t *testing.T) {
		credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
		_, err := app.db.GetCodeSessionCredentialContextForIssue(
			context.Background(),
			credential.OrganizationID+1,
			credential.WorkspaceID,
			credential.CodeSessionID,
		)
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("issue credential with mismatched organization error = %v, want ErrNotFound", err)
		}
		_, err = app.db.GetCodeSessionCredentialContextForIssue(
			context.Background(),
			credential.OrganizationID,
			credential.WorkspaceID+1,
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
		if _, err := app.db.Pool.Exec(context.Background(), `
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
		if err := app.db.Pool.QueryRow(context.Background(), `select status from sessions where id = $1`, credential.PublicSessionID).Scan(&previousStatus); err != nil {
			t.Fatalf("load public session status: %v", err)
		}
		t.Cleanup(func() {
			_, _ = app.db.Pool.Exec(context.Background(), `update sessions set status = $2 where id = $1`, credential.PublicSessionID, previousStatus)
		})
		if _, err := app.db.Pool.Exec(context.Background(), `update sessions set status = 'terminated' where id = $1`, credential.PublicSessionID); err != nil {
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
	cfg.AnthropicUpstreamBaseURL = upstream.URL
	cfg.AnthropicUpstreamAPIKey = "sk-ant-messages-upstream"
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
		if _, err := app.db.Pool.Exec(context.Background(), `
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
	if first.Path != "/v1/messages" || first.Query != "beta=true" || first.APIKey != cfg.AnthropicUpstreamAPIKey {
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
		if request.APIKey != cfg.AnthropicUpstreamAPIKey {
			t.Fatalf("upstream API key = %q, want configured key", request.APIKey)
		}
	}
}

type messagesCodeSessionCredential struct {
	Token           string
	CodeSessionID   string
	PublicSessionID int64
	OrganizationID  int64
	WorkspaceID     int64
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
	var sessionID int64
	var sessionExternalID string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select id, external_id
		from sessions
		where workspace_id = $1 and organization_id = $2 and deleted_at is null
		order by id
		limit 1
	`, apiKey.WorkspaceID, apiKey.OrganizationID).Scan(&sessionID, &sessionExternalID); err != nil {
		t.Fatalf("load Messages credential public session: %v", err)
	}
	now := time.Now().UTC()
	_, err = app.db.CreateCodeSession(context.Background(), db.CreateCodeSessionInput{
		ExternalID:            codeSessionID,
		OrganizationID:        apiKey.OrganizationID,
		WorkspaceID:           apiKey.WorkspaceID,
		SessionID:             sessionID,
		SessionExternalID:     sessionExternalID,
		EnvironmentID:         1,
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
		Token:           token,
		CodeSessionID:   codeSessionID,
		PublicSessionID: sessionID,
		OrganizationID:  apiKey.OrganizationID,
		WorkspaceID:     apiKey.WorkspaceID,
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

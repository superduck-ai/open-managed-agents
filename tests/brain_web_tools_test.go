package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webtools"
)

type webProviderTransport func(*http.Request) (*http.Response, error)

func (f webProviderTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newWebToolsApp(t *testing.T, tools, network string) (*testApp, db.CodeSession, string, *atomic.Int32) {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("web-tools"))
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"web-test","tools":`+tools+`}`)
	env := createEnvironment(t, app, `{"name":"web-test","config":`+network+`}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	id := launchLocalCodeSession(t, app, session.ID)
	registerCodeSessionWorker(t, app, id)
	worker, found, err := app.db.GetCodeSession(t.Context(), id)
	if err != nil || !found {
		t.Fatalf("worker: %v", err)
	}
	var calls atomic.Int32
	provider := webtools.NewClient("test-provider-key", &http.Client{Transport: webProviderTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host != "api.deepseek.com" || r.Header.Get("Authorization") != "Bearer test-provider-key" {
			t.Error("wrong provider authentication")
		}
		body := `{"title":"Source","content":"# Retrieved body"}`
		if strings.HasSuffix(r.URL.Path, "/search") {
			body = `{"results":[{"title":"Source","url":"https://example.com","summary":"summary","snippet":"snippet","date":null}]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	handler := codesessions.NewHandler(app.cfg, codesessions.NewServiceWithCredentials(app.db, app.credentials, nil), app.sandboxTimeouts, nil).WithWebTools(provider)
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	app.baseURL = gateway.URL
	return app, worker, codeSessionIngressToken(t, app, id), &calls
}

func TestBrainWebToolsRejectInvalidIngressAndRetiredEpoch(t *testing.T) {
	app, worker, token, calls := newWebToolsApp(t, `[{"type":"agent_toolset_20260401"}]`, `{"type":"cloud"}`)
	foreign := codeSessionIngressTokenWithScope(t, app, worker.ExternalID, worker.OrganizationUUID, uuid.NewV4().String())
	request := func(credential string) int {
		response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/web/search", strings.NewReader(`{"query":"test"}`), credential, false)
		defer response.Body.Close()
		return response.StatusCode
	}
	for _, credential := range []string{"", foreign} {
		if status := request(credential); status != 401 {
			t.Errorf("invalid ingress=%d", status)
		}
	}
	public, _, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.RotateManagedAgentCodeSessionCredentials(t.Context(), public, worker.ExternalID, "replacement"); err != nil {
		t.Fatal(err)
	}
	if status := request(token); status != 401 && status != 409 {
		t.Errorf("stale epoch=%d", status)
	}
	if calls.Load() != 0 {
		t.Error("invalid credentials reached provider")
	}
}

func TestBrainWebToolsEnforcePermissionsAndNetworking(t *testing.T) {
	for _, tc := range []struct {
		name, tools, network, operation, body string
		status                                int
	}{
		{"disabled", `[{"type":"agent_toolset_20260401","default_config":{"enabled":false}}]`, `{"type":"cloud"}`, "search", `{"query":"test"}`, 403},
		{"limited fetch", `[{"type":"agent_toolset_20260401"}]`, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["example.com"]}}`, "fetch", `{"url":"https://example.com"}`, 403},
		{"invalid body", `[{"type":"agent_toolset_20260401"}]`, `{"type":"cloud"}`, "search", `{"query":"test","count":0}`, 400},
		{"private URL", `[{"type":"agent_toolset_20260401"}]`, `{"type":"cloud"}`, "fetch", `{"url":"http://127.0.0.1"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, worker, token, calls := newWebToolsApp(t, tc.tools, tc.network)
			response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/web/"+tc.operation, strings.NewReader(tc.body), token, false)
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				raw, _ := io.ReadAll(response.Body)
				t.Fatalf("status=%d want=%d body=%s", response.StatusCode, tc.status, raw)
			}
			if calls.Load() != 0 {
				t.Error("denied request reached provider")
			}
		})
	}
}

func TestBrainWebToolsExecuteDocumentedSearchAndFetchThroughServer(t *testing.T) {
	app, worker, token, calls := newWebToolsApp(t, `[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"always_allow"}}}]`, `{"type":"cloud"}`)
	for _, tc := range []struct{ operation, body, want string }{
		{"search", `{"query":"test"}`, "https://example.com"},
		{"fetch", `{"url":"https://example.com"}`, "# Retrieved body"},
		{"fetch", `{"url":"https://example.com"}`, "# Retrieved body"},
	} {
		response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/web/"+tc.operation, strings.NewReader(tc.body), token, false)
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || !strings.Contains(string(raw), tc.want) || strings.Contains(string(raw), "test-provider-key") {
			t.Fatalf("status=%d body=%s", response.StatusCode, raw)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("provider calls=%d want search and two independent fetches", calls.Load())
	}
}

//go:build integration || e2e || e2b_integration

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
	transport := webProviderTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host == "api.deepseek.com" && r.Header.Get("Authorization") != "Bearer test-provider-key" {
			t.Error("wrong provider authentication")
		}
		body := "# Retrieved body"
		if r.URL.Host != "api.deepseek.com" && r.Header.Get("Authorization") != "" {
			t.Error("credential leaked to page")
		}
		if strings.HasSuffix(r.URL.Path, "/search") {
			body = `{"results":[{"title":"Source","url":"https://example.com","summary":"summary","snippet":"snippet","date":null}]}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	provider := webtools.NewClient(webtools.NewDeepSeekSearch("test-provider-key", &http.Client{Transport: transport}), transport)
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
	if status := request(token); status != 409 {
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
		{"limited fetch", `[{"type":"agent_toolset_20260401"}]`, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["example.com"]}}`, "fetch", `{"url":"https://blocked.example.net"}`, 403},
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

func TestBrainWebFetchLimitedAllowsApprovedHost(t *testing.T) {
	app, worker, token, calls := newWebToolsApp(t, `[{"type":"agent_toolset_20260401","configs":[{"name":"web_fetch","allowed_domains":["example.com"],"max_content_tokens":20}]}]`, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["example.com"]}}`)
	response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/web/fetch", strings.NewReader(`{"url":"https://example.com"}`), token, false)
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || !strings.Contains(string(raw), "Retrieved body") || calls.Load() != 1 {
		t.Fatalf("%d %s calls=%d", response.StatusCode, raw, calls.Load())
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v2/ccr-sessions/"+worker.ExternalID+"/web/fetch", strings.NewReader(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-OMA-Session-Thread-ID", "foreign-thread")
	rejected, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer rejected.Body.Close()
	if rejected.StatusCode < 400 || calls.Load() != 1 {
		t.Fatal("invalid thread reached network")
	}
}

func TestBrainWebDomainConfigurationContracts(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("web-domain-contracts"))
	env := createEnvironment(t, app, `{"name":"web-contracts","config":{"type":"cloud"}}`)
	for _, name := range []string{"web_search", "web_fetch"} {
		for _, tc := range []struct {
			value   string
			status  int
			message string
		}{
			{`["a.localdomain"]`, 400, "allowed_domains.0:"},
			{`["127.0.0.1"]`, 400, "allowed_domains.0:"},
			{`[]`, 400, "allowed_domains: Empty list"},
			{`["a_b.example.com/"]`, 200, ""},
		} {
			t.Run(name+tc.value, func(t *testing.T) {
				base := createAgent(t, app, `{"name":"base","model":"claude-opus-4-6"}`)
				session := createSession(t, app, `{"agent":`+quoteJSON(base.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
				tools := `[{"type":"agent_toolset_20260401","configs":[{"name":"` + name + `","allowed_domains":` + tc.value + `}]}]`
				for _, request := range []struct{ path, body string }{
					{"/v1/agents", `{"name":"contract","model":"claude-opus-4-6","tools":` + tools + `}`},
					{"/v1/agents/" + base.ID, `{"version":1,"tools":` + tools + `}`},
					{"/v1/sessions", `{"environment_id":` + quoteJSON(env.ID) + `,"agent":{"type":"agent_with_overrides","id":` + quoteJSON(base.ID) + `,"tools":` + tools + `}}`},
					{"/v1/sessions/" + session.ID, `{"agent":{"tools":` + tools + `}}`},
				} {
					response := doSessionRequest(t, app, http.MethodPost, request.path, strings.NewReader(request.body), defaultTestKey, true)
					body := readAll(t, response.Body)
					response.Body.Close()
					if response.StatusCode != tc.status || !strings.Contains(string(body), tc.message) {
						t.Fatalf("%s: status=%d body=%s", request.path, response.StatusCode, body)
					}
				}
			})
		}
	}
}

func TestBrainWebDisjointChildPoliciesRejectBeforeProvider(t *testing.T) {
	app, worker, token, calls := newWebToolsApp(t, `[{"type":"agent_toolset_20260401","configs":[{"name":"web_search","allowed_domains":["example.com"]},{"name":"web_fetch","allowed_domains":["example.com"]}]}]`, `{"type":"cloud"}`)
	primary, found, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("primary thread: %v", err)
	}
	child := primary
	child.UUID = uuid.NewV4().String()
	child.ExternalID = "sthr_web_disjoint"
	child.ParentThreadUUID = &primary.UUID
	child.ParentThreadExternalID = &primary.ExternalID
	child.AgentSnapshot = []byte(`{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"web_search","allowed_domains":["iana.org"]},{"name":"web_fetch","allowed_domains":["iana.org"]}]}]}`)
	if _, err := app.db.CreateSessionThreadIfAbsent(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ method, body string }{{"search", `{"query":"test"}`}, {"fetch", `{"url":"https://example.com"}`}} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v2/ccr-sessions/"+worker.ExternalID+"/web/"+input.method, strings.NewReader(input.body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-OMA-Session-Thread-ID", child.ExternalID)
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != 403 || !strings.Contains(string(body), "url_not_allowed") {
			t.Fatalf("%s: %d %s", input.method, response.StatusCode, body)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("denied child called provider %d times", calls.Load())
	}
}

func TestBrainWebUserLocationConfigurationContracts(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("web-location-contracts"))
	env := createEnvironment(t, app, `{"name":"web-location","config":{"type":"cloud"}}`)
	for _, tc := range []struct {
		location string
		status   int
	}{
		{`{"type":"precise"}`, 400},
		{`{"type":"approximate","country":42}`, 400},
		{`{"type":"approximate","country":"US","city":"San Francisco","region":"California","timezone":"America/Los_Angeles"}`, 200},
	} {
		t.Run(tc.location, func(t *testing.T) {
			base := createAgent(t, app, `{"name":"base","model":"claude-opus-4-6"}`)
			session := createSession(t, app, `{"agent":`+quoteJSON(base.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			tools := `[{"type":"agent_toolset_20260401","configs":[{"name":"web_search","user_location":` + tc.location + `}]}]`
			for _, request := range []struct{ path, body string }{
				{"/v1/agents", `{"name":"location","model":"claude-opus-4-6","tools":` + tools + `}`},
				{"/v1/agents/" + base.ID, `{"version":1,"tools":` + tools + `}`},
				{"/v1/sessions", `{"environment_id":` + quoteJSON(env.ID) + `,"agent":{"type":"agent_with_overrides","id":` + quoteJSON(base.ID) + `,"tools":` + tools + `}}`},
				{"/v1/sessions/" + session.ID, `{"agent":{"tools":` + tools + `}}`},
			} {
				response := doSessionRequest(t, app, http.MethodPost, request.path, strings.NewReader(request.body), defaultTestKey, true)
				body := readAll(t, response.Body)
				response.Body.Close()
				if response.StatusCode != tc.status {
					t.Fatalf("%s: status=%d body=%s", request.path, response.StatusCode, body)
				}
				if tc.status == 200 && (!strings.Contains(string(body), `"user_location"`) || !strings.Contains(string(body), "America/Los_Angeles")) {
					t.Fatalf("%s: location was not preserved: %s", request.path, body)
				}
			}
		})
	}
}

//go:build integration || e2e || e2b_integration

package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

const portToolEnabled = `[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url","enabled":true}]}]`

func portToolSettings(t *testing.T) *sessionports.Settings {
	t.Helper()
	codec, err := sessionports.NewLabelCodec(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return sessionports.NewWithCodec(config.SessionPortsConfig{DomainSuffix: "preview.example.com", URLScheme: "https", BlockedPorts: []int{9229}}, 3001, codec)
}

func newPortToolApp(t *testing.T, tools string, settings *sessionports.Settings) (*testApp, db.CodeSession, string) {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("port-tools"))
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"port-test","tools":`+tools+`}`)
	env := createEnvironment(t, app, `{"name":"port-test","config":{"type":"cloud"}}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	id := launchLocalCodeSession(t, app, session.ID)
	registerCodeSessionWorker(t, app, id)
	worker, found, err := app.db.GetCodeSession(t.Context(), id)
	if err != nil || !found {
		t.Fatalf("worker: %v", err)
	}
	handler := codesessions.NewHandler(app.cfg, codesessions.NewServiceWithCredentials(app.db, app.credentials, nil), app.sandboxTimeouts, nil).WithSessionPorts(settings)
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	app.baseURL = gateway.URL
	return app, worker, codeSessionIngressToken(t, app, id)
}

func requestPortURL(t *testing.T, app *testApp, worker db.CodeSession, token, body string) (int, string) {
	t.Helper()
	response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/port-url", strings.NewReader(body), token, false)
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(raw)
}

func TestBrainPortToolRejectsUnavailableRequests(t *testing.T) {
	for _, testCase := range []struct {
		name, tools, body, message string
		disabled                   bool
		status                     int
	}{
		{"not configured", `[{"type":"agent_toolset_20260401","default_config":{"enabled":true}}]`, `{"port":4000}`, "get_port_url is not enabled", false, 403},
		{"configured disabled", `[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url","enabled":false}]}]`, `{"port":4000}`, "get_port_url is not enabled", false, 403},
		{"feature disabled", portToolEnabled, `{"port":4000}`, "not enabled on this server", true, 409},
		{"string port", portToolEnabled, `{"port":"4000"}`, "between 1 and 65535", false, 400},
		{"extra field", portToolEnabled, `{"port":4000,"session_id":"sesn_other"}`, "between 1 and 65535", false, 400},
		{"out of range", portToolEnabled, `{"port":70000}`, "between 1 and 65535", false, 400},
		{"envd", portToolEnabled, `{"port":49983}`, "port 49983 is reserved", false, 400},
		{"sandbox mcp", portToolEnabled, `{"port":3001}`, "port 3001 is reserved", false, 400},
		{"configured block", portToolEnabled, `{"port":9229}`, "port 9229 is reserved", false, 400},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			settings := portToolSettings(t)
			if testCase.disabled {
				settings = nil
			}
			app, worker, token := newPortToolApp(t, testCase.tools, settings)
			status, body := requestPortURL(t, app, worker, token, testCase.body)
			if status != testCase.status || !strings.Contains(body, testCase.message) || strings.Contains(body, "preview.example.com") {
				t.Fatalf("status=%d body=%s", status, body)
			}
		})
	}
}

func TestBrainPortToolRejectsInvalidIngress(t *testing.T) {
	app, worker, _ := newPortToolApp(t, portToolEnabled, portToolSettings(t))
	if status, body := requestPortURL(t, app, worker, "", `{"port":4000}`); status != 401 {
		t.Fatalf("status=%d body=%s", status, body)
	}
}

func TestBrainPortToolReturnsPublicAddress(t *testing.T) {
	settings := portToolSettings(t)
	app, worker, token := newPortToolApp(t, portToolEnabled, settings)
	status, body := requestPortURL(t, app, worker, token, `{"port":4000}`)
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	session, _, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := settings.URL(session.UUID, 4000)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Type string `json:"type"`
		Port int    `json:"port"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil || result.Type != "session_port" || result.Port != 4000 || result.URL != want {
		t.Fatalf("result=%+v want url %s err=%v", result, want, err)
	}
}

func TestBrainPortToolFollowsThreadSnapshots(t *testing.T) {
	app, worker, token := newPortToolApp(t, portToolEnabled, portToolSettings(t))
	primary, found, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("primary thread: %v", err)
	}
	for _, testCase := range []struct {
		id, snapshot string
		status       int
	}{
		{"sthr_port_denied", `{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":true}}]}`, 403},
		{"sthr_port_allowed", `{"tools":` + portToolEnabled + `}`, 200},
	} {
		child := primary
		child.UUID = uuid.NewV4().String()
		child.ExternalID = testCase.id
		child.ParentThreadUUID = &primary.UUID
		child.ParentThreadExternalID = &primary.ExternalID
		child.AgentSnapshot = []byte(testCase.snapshot)
		if _, err := app.db.CreateSessionThreadIfAbsent(t.Context(), child); err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v2/ccr-sessions/"+worker.ExternalID+"/port-url", strings.NewReader(`{"port":4000}`))
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
		if response.StatusCode != testCase.status {
			t.Fatalf("%s: %d %s", testCase.id, response.StatusCode, body)
		}
	}
}

func TestAgentToolsWithoutPortToolKeepPublicShape(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("port-tool-shape"))
	want := `[{"type":"agent_toolset_20260401","configs":[{"name":"ask_user_question","type":"ask_user_question","enabled":false,"permission_policy":{"type":"always_allow"}}],"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}]`
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"shape","tools":[{"type":"agent_toolset_20260401"}]}`)
	for _, method := range []string{"create", "retrieve"} {
		var tools json.RawMessage
		if method == "create" {
			tools = agentTools(t, app, http.MethodPost, "/v1/agents", `{"model":"claude-opus-4-6","name":"shape","tools":[{"type":"agent_toolset_20260401"}]}`)
		} else {
			tools = agentTools(t, app, http.MethodGet, "/v1/agents/"+agent.ID, "")
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, tools); err != nil {
			t.Fatal(err)
		}
		if compact.String() != want {
			t.Fatalf("%s tools changed\n got %s\nwant %s", method, compact.String(), want)
		}
	}
}

func agentTools(t *testing.T, app *testApp, method, path, body string) json.RawMessage {
	t.Helper()
	response := doSessionRequest(t, app, method, path, strings.NewReader(body), defaultTestKey, true)
	raw := readAll(t, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, raw)
	}
	var agent struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(raw, &agent); err != nil {
		t.Fatal(err)
	}
	return agent.Tools
}

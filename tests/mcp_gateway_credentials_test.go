//go:build e2e

package tests

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// Uses an isolated PostgreSQL database and the real HTTP authentication boundary;
// no model, sandbox, object store, or NATS service is needed for credential checks.
func TestManagedTunnelGatewayCredentials(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.URL = managedTunnelDatabase(t, cfg.Database.URL)
	cfg.Tunnel.PublicBaseURL = "https://oma.example"
	cfg.CodeSession.SandboxAPIBaseURL = "http://gateway.example"
	app := newTestAppWithStore(t, &cfg, newFakeStore("mcp-gateway-credentials"))
	t.Cleanup(app.close)
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"gateway-credential-proof","mcp_servers":[{"type":"url","name":"local","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/sse"},{"type":"url","name":"remote","url":"https://docs.example/sse"}]}`)
	environment := createEnvironment(t, app, `{"name":"gateway-credential-proof"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	codeID := launchLocalCodeSession(t, app, session.ID)
	oldToken := codeSessionIngressToken(t, app, codeID)
	claims, err := app.credentials.Verify(oldToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.WorkerEpoch <= 0 {
		t.Fatal("managed session did not issue a positive epoch")
	}
	_, err = app.db.RotateManagedAgentCodeSessionCredentials(t.Context(), db.Session{
		OrganizationUUID: claims.OrganizationUUID, WorkspaceUUID: claims.WorkspaceUUID, ExternalID: session.ID,
	}, codeID, "gateway-test-oauth-hash")
	if err != nil {
		t.Fatal(err)
	}
	currentToken := codeSessionIngressToken(t, app, codeID)
	identity := codesessions.SessionCredentialIdentity{
		SessionID: codeID, PublicSessionID: session.ID, AgentID: agent.ID, AgentVersion: claims.AgentVersion,
		OrganizationUUID: claims.OrganizationUUID, WorkspaceUUID: claims.WorkspaceUUID,
	}
	noEpoch, err := app.credentials.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, codeID, token string
		status              int
	}{
		{"missing token", codeID, "", http.StatusUnauthorized},
		{"old epoch", codeID, oldToken, http.StatusUnauthorized},
		{"missing epoch", codeID, noEpoch, http.StatusUnauthorized},
		{"other session", "cse_other", currentToken, http.StatusUnauthorized},
		{"other organization", codeID, codeSessionIngressTokenWithScope(t, app, codeID, "10000000-0000-4000-8000-000000000001", ""), http.StatusUnauthorized},
		{"other workspace", codeID, codeSessionIngressTokenWithScope(t, app, codeID, "", "10000000-0000-4000-8000-000000000002"), http.StatusUnauthorized},
		// Current credentials reach named-server authorization, which rejects this
		// absent Snapshot entry instead of granting arbitrary target access.
		{"current token with unconfigured server", codeID, currentToken, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, prefix := range []string{"/v2/ccr-sessions/", "/.well-known/oauth-protected-resource/v2/ccr-sessions/"} {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, app.baseURL+prefix+test.codeID+"/mcp/missing", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+test.token)
				response, err := app.client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body := readAll(t, response.Body)
				response.Body.Close()
				if response.StatusCode != test.status {
					t.Fatalf("%s: status %d, want %d: %s", prefix, response.StatusCode, test.status, body)
				}
			}
		})
	}
	assertMCPRuntimeContext(t, app, codeID, oldToken, http.StatusUnauthorized)
	assertMCPRuntimeContext(t, app, codeID, currentToken, http.StatusOK)
}

// Verify session_context through the HTTP credential boundary as well as the
// Gateway: it must rebuild from stored declarations and never cache credentials.
func assertMCPRuntimeContext(t *testing.T, app *testApp, codeID, token string, wantStatus int) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, app.baseURL+"/v2/sessions/"+codeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("session_context status=%d want=%d", response.StatusCode, wantStatus)
	}
	if wantStatus != http.StatusOK {
		return
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("session_context credential response may be cached")
	}
	var body struct {
		Context struct {
			MCP struct {
				Servers map[string]struct {
					Type    string            `json:"type"`
					URL     string            `json:"url"`
					Headers map[string]string `json:"headers"`
				} `json:"mcpServers"`
			} `json:"mcp_config"`
		} `json:"session_context"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	local, remote := body.Context.MCP.Servers["local"], body.Context.MCP.Servers["remote"]
	if local.Type != "http" || local.URL != "http://gateway.example/v2/ccr-sessions/"+codeID+"/mcp/local" || local.Headers["Authorization"] != "Bearer "+token {
		t.Fatal("session_context did not build the current Tunnel connection")
	}
	if remote.Type != "sse" || remote.URL != "https://docs.example/sse" || len(remote.Headers) != 0 {
		t.Fatal("session_context changed the ordinary MCP connection")
	}
}

package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

// This joins real HTTP resource writes, Runner CodeSession binding, credential
// authentication and the same outbound rewriting module used by CONNECT MITM.
// The transport itself is covered by codesessions' TLS/WebSocket tunnel tests.
func TestGitResourcesProxyAPI(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.CodeSession.UpstreamProxyMITMEnabled = true
	app := newTestAppWithStore(t, &cfg, newFakeStore("git-resource-proxy-api"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"git-proxy-api-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	environment := createEnvironment(t, app, `{"name":"git-proxy-api-environment"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`,"resources":[{"type":"github_repository","url":"https://github.com/example/repo","authorization_token":"github-proxy-secret"}]}`)
	defer deleteSession(t, app, session.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	ingressToken := codeSessionIngressToken(t, app, codeSessionID)
	claims, err := app.credentials.Verify(ingressToken)
	if err != nil || claims.PublicSessionID != session.ID {
		t.Fatalf("invalid CodeSession credential association: %v", err)
	}
	// Exercise the real HTTP authentication entry point, without dialing GitHub.
	connection := dialCCRV2UpstreamProxy(t, app, ingressToken)
	connection.Close()
	assertCCRV2UpstreamProxyAuthRejected(t, app, "not-a-code-session-token")
	egress := vaults.NewMITMEgress(app.db, app.vaultSecrets, nil, nil)
	identity := vaults.EgressSession{CodeSessionExternalID: claims.SessionID, OrganizationUUID: claims.OrganizationUUID, WorkspaceUUID: claims.WorkspaceUUID}
	prepare := func(target, path string) (*http.Request, error) {
		request, err := http.NewRequest(http.MethodGet, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = egress.Prepare(context.Background(), identity, target, request, http.DefaultTransport)
		return request, err
	}
	for _, test := range []struct{ authority, path string }{
		{"github.com:443", "/example/other.git/info/refs?service=git-upload-pack"},
		{"github.com:443", "/example/repo-extra.git/info/refs?service=git-upload-pack"},
		{"github.com:443", "/example/repo/issues"},
		{"github.com:443", "/example/%72epo.git/info/refs?service=git-upload-pack"},
		{"github.com:444", "/example/repo.git/info/refs?service=git-upload-pack"},
	} {
		request, err := prepare(test.authority, test.path)
		if err == nil && request.Header.Get("Authorization") != "" {
			t.Fatal("credential escaped exact repository scope")
		}
	}
	request, err := prepare("github.com:443", "/example/repo.git/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	_, token, ok := request.BasicAuth()
	if !ok || token != "github-proxy-secret" {
		t.Fatal("resource token was not injected")
	}
	row := mustGitResourceRows(t, app, session.ID)[0]
	response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/resources/"+row.ExternalID+"?beta=true", strings.NewReader(`{"authorization_token":"github-rotated-secret"}`), defaultTestKey, true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("rotate status=%d", response.StatusCode)
	}
	response.Body.Close()
	request, err = prepare("github.com:443", "/example/repo.git/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	_, token, ok = request.BasicAuth()
	if !ok || token != "github-rotated-secret" {
		t.Fatal("proxy reused a stale token after rotation")
	}
	archiveSession(t, app, session.ID)
	if _, err := prepare("github.com:443", "/example/repo.git/info/refs?service=git-upload-pack"); err == nil {
		t.Fatal("archived session retained Git credentials")
	}
}

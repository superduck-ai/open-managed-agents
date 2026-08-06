package codesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"

	"github.com/go-chi/chi/v5"
)

func TestParseMCPProxyTargetRejectsInvalidTargets(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
	}{
		{name: "missing", query: "other=value"},
		{name: "duplicate", query: "mcp_url=https%3A%2F%2Fone.example&mcp_url=https%3A%2F%2Ftwo.example"},
		{name: "relative", query: "mcp_url=%2Fapi%2Fmcp"},
		{name: "unsupported scheme", query: "mcp_url=ftp%3A%2F%2Fexample.test%2Fmcp"},
		{name: "embedded credentials", query: "mcp_url=https%3A%2F%2Fuser%3Asecret%40example.test%2Fmcp"},
		{name: "fragment", query: "mcp_url=https%3A%2F%2Fexample.test%2Fmcp%23fragment"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := parseMCPProxyTarget(test.query); err == nil {
				t.Fatal("parseMCPProxyTarget() error = nil, want error")
			}
		})
	}
}

func TestMCPProxyAuthorizationRejectsURLNotInSession(t *testing.T) {
	snapshot := json.RawMessage(`{"mcp_servers":[{"type":"http","url":"https://configured.example/mcp"}]}`)
	policy, err := networkpolicy.ParseMCPProxyPolicy(json.RawMessage(`{"type":"cloud","networking":{"type":"unrestricted"}}`), snapshot)
	if err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	handler := &Handler{
		logger: slog.Default(),
		loadMCPPolicyContext: func(context.Context, upstreamProxyIdentity) (mcpProxyPolicyContext, error) {
			return mcpProxyPolicyContext{policy: policy}, nil
		},
	}
	target, err := url.Parse("https://other.example/mcp")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	if handler.authorizeMCPProxyTarget(context.Background(), testUpstreamProxyIdentity(), target, target.String()) {
		t.Fatal("unconfigured MCP URL must be denied")
	}
}

func TestMCPProxyForwardsProtocolHeadersAndUsesCredentialInjector(t *testing.T) {
	type capturedRequest struct {
		method  string
		uri     string
		host    string
		headers http.Header
		body    string
	}
	captured := make(chan capturedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured <- capturedRequest{method: r.Method, uri: r.URL.RequestURI(), host: r.Host, headers: r.Header.Clone(), body: string(body)}
		w.Header().Set("Mcp-Session-Id", "upstream-session")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	targetURL := upstream.URL + "/api/mcp?tenant=one"
	snapshot, err := json.Marshal(map[string]any{"mcp_servers": []any{map[string]any{"type": "http", "url": targetURL}}})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	policy, err := networkpolicy.ParseMCPProxyPolicy(json.RawMessage(`{"type":"cloud","networking":{"type":"unrestricted"}}`), snapshot)
	if err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	credentials, err := NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create credentials: %v", err)
	}
	service := NewServiceWithCredentials(nil, credentials, nil)
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	handler := NewHandler(config.Config{}, service, nil, logger)
	handler.mcpProxyTransport = http.DefaultTransport
	handler.loadMCPPolicyContext = func(context.Context, upstreamProxyIdentity) (mcpProxyPolicyContext, error) {
		return mcpProxyPolicyContext{
			policy: policy,
			proxyPolicyScope: proxyPolicyScope{
				organizationUUID:      "00000000-0000-0000-0000-000000000001",
				workspaceUUID:         "00000000-0000-0000-0000-000000000002",
				environmentExternalID: "env_test",
			},
		}, nil
	}
	handler.injectMCPProxyHeaders = func(_ context.Context, claims SessionCredentialClaims, target *url.URL, headers http.Header) error {
		if claims.SessionID != "cse_test" || target.String() != targetURL {
			t.Errorf("credential injector context = session %q target %q", claims.SessionID, target)
		}
		headers.Set("Authorization", "Bearer upstream-secret")
		headers.Set("X-MCP-Key", "vault-secret")
		return nil
	}
	token, err := credentials.Issue(SessionCredentialIdentity{
		SessionID:        "cse_test",
		PublicSessionID:  "sesn_test",
		AgentID:          "agent_test",
		AgentVersion:     1,
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
	})
	if err != nil {
		t.Fatalf("issue session token: %v", err)
	}

	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	proxyServer := httptest.NewServer(router)
	t.Cleanup(proxyServer.Close)
	proxyURL := proxyServer.URL + "/v2/ccr-sessions/cse_test/mcp?" + url.Values{"mcp_url": {targetURL}}.Encode()
	request, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewBufferString(`{"jsonrpc":"2.0","method":"initialize"}`))
	if err != nil {
		t.Fatalf("create proxy request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Session-Id", "client-session")
	request.Header.Set("Proxy-Authorization", "must-not-leak")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read proxy response: %v", err)
	}
	if response.StatusCode != http.StatusAccepted || response.Header.Get("Mcp-Session-Id") != "upstream-session" || !strings.Contains(string(responseBody), `"result"`) {
		t.Fatalf("proxy response = status %d headers %#v body %s", response.StatusCode, response.Header, responseBody)
	}

	got := <-captured
	if got.method != http.MethodPost || got.uri != "/api/mcp?tenant=one" || got.host != strings.TrimPrefix(upstream.URL, "http://") || !strings.Contains(got.body, `"initialize"`) {
		t.Fatalf("upstream request = %#v", got)
	}
	if got.headers.Get("Authorization") != "Bearer upstream-secret" || got.headers.Get("X-MCP-Key") != "vault-secret" {
		t.Fatalf("upstream credentials = %#v", got.headers)
	}
	if got.headers.Get("Proxy-Authorization") != "" || got.headers.Get("Mcp-Session-Id") != "client-session" {
		t.Fatalf("upstream protocol headers = %#v", got.headers)
	}

	var requestLog map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logOutput.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		if entry["msg"] == "MCP proxy request received" {
			requestLog = entry
			break
		}
	}
	if requestLog == nil {
		t.Fatal("MCP proxy request log not found")
	}
	if requestLog["code_session_id"] != "cse_test" || requestLog["method"] != http.MethodPost || requestLog["mcp_url"] != upstream.URL+"/api/mcp" || requestLog["content_type"] != "application/json" {
		t.Fatalf("MCP proxy request log = %#v", requestLog)
	}
	if strings.Contains(logOutput.String(), "tenant=one") || strings.Contains(logOutput.String(), "upstream-secret") {
		t.Fatalf("MCP proxy request log leaked sensitive data: %s", logOutput.String())
	}
}

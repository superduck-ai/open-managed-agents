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
	"github.com/superduck-ai/open-managed-agents/internal/vaults"

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

func TestNamedMCPProxyDispatchesOnlyThroughTunnelInvokerAndSeparatesCredentials(t *testing.T) {
	targetURL := "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"
	handler, mcpToken, sessionToken := newNamedMCPTestHandler(t, "local_tunnel", targetURL)
	var tunnelRequest *http.Request
	var tunnelTarget *url.URL
	handler.tunnelInvoker = tunnelInvokerFuncs{
		serve: func(w http.ResponseWriter, request *http.Request, _, _ string, target *url.URL) bool {
			tunnelRequest = request.Clone(request.Context())
			tunnelTarget = target
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{}}`))
			return true
		},
	}
	handler.mcpProxyTransport = mcpRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("named Tunnel request reached ordinary MCP outbound transport")
		return nil, nil
	})
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)

	request := httptest.NewRequest(http.MethodPost, "/v2/ccr-sessions/cse_test/mcp/local_tunnel", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+sessionToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("named MCP proxy accepted session token: status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v2/ccr-sessions/cse_test/mcp/missing", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+mcpToken)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing named MCP server status = %d, want 404", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v2/ccr-sessions/cse_test/mcp/%20local_tunnel%20", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+mcpToken)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("non-canonical named MCP server status = %d, want 404", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/v2/ccr-sessions/cse_test/mcp/local_tunnel", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+mcpToken)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("named MCP Tunnel status = %d body = %s", response.Code, response.Body.String())
	}
	if tunnelTarget == nil || tunnelTarget.String() != targetURL {
		t.Fatalf("named MCP Tunnel target = %#v", tunnelTarget)
	}
	if tunnelRequest == nil || tunnelRequest.Header.Get("Authorization") != "" {
		t.Fatalf("MCP capability leaked to TunnelInvoker: %#v", tunnelRequest)
	}
}

func TestNamedMCPProxyRejectsOrdinaryMCPWithoutOutboundRequest(t *testing.T) {
	handler, mcpToken, _ := newNamedMCPTestHandler(t, "docs", "https://mcp.example.test/api/mcp")
	handler.tunnelInvoker = tunnelInvokerFuncs{serve: func(http.ResponseWriter, *http.Request, string, string, *url.URL) bool {
		return false
	}}
	handler.mcpProxyTransport = mcpRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("ordinary MCP named Gateway request reached outbound transport")
		return nil, nil
	})
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	request := httptest.NewRequest(http.MethodPost, "/v2/ccr-sessions/cse_test/mcp/docs", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+mcpToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("ordinary named MCP status = %d, want 404: %s", response.Code, response.Body.String())
	}
}

func TestNamedMCPProxySupportsEscapedServerNamePath(t *testing.T) {
	targetURL := "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"
	handler, mcpToken, _ := newNamedMCPTestHandler(t, "team/tools", targetURL)
	var tunnelTarget *url.URL
	handler.tunnelInvoker = tunnelInvokerFuncs{serve: func(w http.ResponseWriter, _ *http.Request, _, _ string, target *url.URL) bool {
		tunnelTarget = target
		w.WriteHeader(http.StatusNoContent)
		return true
	}}
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	request := httptest.NewRequest(http.MethodPost, "/v2/ccr-sessions/cse_test/mcp/team%2Ftools", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+mcpToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("escaped named MCP server status = %d, want 204: %s", response.Code, response.Body.String())
	}
	if tunnelTarget == nil || tunnelTarget.String() != targetURL {
		t.Fatalf("escaped named MCP Tunnel target = %#v", tunnelTarget)
	}
}

func TestMCPProxyQueryPreservesTunnelDispatch(t *testing.T) {
	targetURL := "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"
	handler, _, sessionToken := newNamedMCPTestHandler(t, "local_tunnel", targetURL)
	var tunnelTarget *url.URL
	handler.tunnelInvoker = tunnelInvokerFuncs{serve: func(w http.ResponseWriter, _ *http.Request, _, _ string, target *url.URL) bool {
		tunnelTarget = target
		w.WriteHeader(http.StatusAccepted)
		return true
	}}
	handler.mcpProxyTransport = mcpRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("query-based Tunnel request reached ordinary MCP outbound transport")
		return nil, nil
	})
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v2/ccr-sessions/cse_test/mcp?"+url.Values{"mcp_url": {targetURL}}.Encode(),
		strings.NewReader(`{}`),
	)
	request.Header.Set("Authorization", "Bearer "+sessionToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("query-based MCP Tunnel status = %d, want 202: %s", response.Code, response.Body.String())
	}
	if tunnelTarget == nil || tunnelTarget.String() != targetURL {
		t.Fatalf("query-based MCP Tunnel target = %#v", tunnelTarget)
	}
}

func TestRuntimeMCPURLsSeparateResourceAndMetadata(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "http://host.docker.internal:38080/request", nil)
	resourceURL := runtimeMCPResourceURL(request, "cse_test", "local_tunnel")
	if resourceURL != "http://host.docker.internal:38080/v2/ccr-sessions/cse_test/mcp/local_tunnel" {
		t.Fatalf("runtime MCP resource URL = %q", resourceURL)
	}
	metadataURL := runtimeMCPMetadataURL(request, "cse_test", "local_tunnel")
	if metadataURL != "http://host.docker.internal:38080/.well-known/oauth-protected-resource/v2/ccr-sessions/cse_test/mcp/local_tunnel" {
		t.Fatalf("runtime MCP metadata URL = %q", metadataURL)
	}
}

func TestMCPGatewayResponseWriterRewritesOnlyChallengeMetadataURL(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	writer := newMCPGatewayResponseWriter(recorder, "https://runtime.example/metadata")
	writer.Header().Set("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="http://private/metadata"`)
	writer.WriteHeader(http.StatusUnauthorized)
	if got := recorder.Header().Get("WWW-Authenticate"); got != `Bearer realm="mcp", resource_metadata="https://runtime.example/metadata"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
}

func TestWriteCapturedMCPDiscoveryRewritesUnauthorizedRuntimeURLs(t *testing.T) {
	t.Parallel()
	captured := newMCPDiscoveryCapture()
	captured.Header().Set("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="http://private.example/metadata"`)
	captured.WriteHeader(http.StatusUnauthorized)
	_, _ = captured.Write([]byte(`{"resource":"http://private.example/mcp","authorization_servers":["https://auth.example"]}`))

	request := httptest.NewRequest(http.MethodGet, "https://runtime.example/discovery", nil)
	recorder := httptest.NewRecorder()
	writeCapturedMCPDiscovery(
		recorder,
		request,
		captured,
		"https://runtime.example/v2/ccr-sessions/cse_test/mcp/local_tunnel",
		"https://runtime.example/.well-known/oauth-protected-resource/v2/ccr-sessions/cse_test/mcp/local_tunnel",
	)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != `Bearer realm="mcp", resource_metadata="https://runtime.example/.well-known/oauth-protected-resource/v2/ccr-sessions/cse_test/mcp/local_tunnel"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
	var metadata map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if got := metadata["resource"]; got != "https://runtime.example/v2/ccr-sessions/cse_test/mcp/local_tunnel" {
		t.Fatalf("resource = %#v", got)
	}
}

func TestNamedMCPProtectedResourceUsesTunnelCapabilityAndPublicResourceURL(t *testing.T) {
	targetURL := "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"
	handler, token, _ := newNamedMCPTestHandler(t, "local_tunnel", targetURL)
	var tunnelRequest *http.Request
	var tunnelTarget *url.URL
	handler.tunnelInvoker = tunnelInvokerFuncs{discovery: func(w http.ResponseWriter, request *http.Request, _, _ string, target *url.URL) bool {
		tunnelRequest = request.Clone(request.Context())
		tunnelTarget = target
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resource":"https://private.example/mcp","authorization_servers":["https://auth.example"]}`))
		return true
	}}
	handler.mcpProxyTransport = mcpRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("Tunnel metadata request reached ordinary MCP outbound transport")
		return nil, nil
	})
	router := chi.NewRouter()
	router.Get(
		"/.well-known/oauth-protected-resource/v2/ccr-sessions/{code_session_id}/mcp/{server_name}",
		handler.HandleMCPProtectedResource,
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"http://host.docker.internal:38080/.well-known/oauth-protected-resource/v2/ccr-sessions/cse_test/mcp/local_tunnel",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("metadata status = %d body = %s", response.Code, response.Body.String())
	}
	var metadata map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	wantResource := "http://host.docker.internal:38080/v2/ccr-sessions/cse_test/mcp/local_tunnel"
	if metadata["resource"] != wantResource {
		t.Fatalf("metadata resource = %#v, want %q", metadata["resource"], wantResource)
	}
	if tunnelTarget == nil || tunnelTarget.String() != targetURL {
		t.Fatalf("Tunnel metadata target = %#v", tunnelTarget)
	}
	if tunnelRequest == nil || tunnelRequest.Header.Get("Authorization") != "" {
		t.Fatalf("MCP capability leaked to Tunnel metadata request: %#v", tunnelRequest)
	}
}

func TestNamedMCPProtectedResourceRejectsOrdinaryMCPWithoutOutboundRequest(t *testing.T) {
	handler, token, _ := newNamedMCPTestHandler(t, "docs", "https://mcp.example.test/api/mcp")
	handler.tunnelInvoker = tunnelInvokerFuncs{discovery: func(http.ResponseWriter, *http.Request, string, string, *url.URL) bool {
		return false
	}}
	handler.mcpProxyTransport = mcpRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("ordinary named metadata request reached outbound transport")
		return nil, nil
	})
	router := chi.NewRouter()
	router.Get(
		"/.well-known/oauth-protected-resource/v2/ccr-sessions/{code_session_id}/mcp/{server_name}",
		handler.HandleMCPProtectedResource,
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"http://host.docker.internal:38080/.well-known/oauth-protected-resource/v2/ccr-sessions/cse_test/mcp/docs",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("ordinary named metadata status = %d, want 404: %s", response.Code, response.Body.String())
	}
}

type mcpRoundTripFunc func(*http.Request) (*http.Response, error)

func (f mcpRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type tunnelInvokerFuncs struct {
	serve     func(http.ResponseWriter, *http.Request, string, string, *url.URL) bool
	discovery func(http.ResponseWriter, *http.Request, string, string, *url.URL) bool
}

func (f tunnelInvokerFuncs) ServeTunnel(
	w http.ResponseWriter,
	r *http.Request,
	organizationUUID string,
	workspaceUUID string,
	target *url.URL,
) bool {
	if f.serve == nil {
		return false
	}
	return f.serve(w, r, organizationUUID, workspaceUUID, target)
}

func (f tunnelInvokerFuncs) ServeTunnelOAuthDiscovery(
	w http.ResponseWriter,
	r *http.Request,
	organizationUUID string,
	workspaceUUID string,
	target *url.URL,
) bool {
	if f.discovery == nil {
		return false
	}
	return f.discovery(w, r, organizationUUID, workspaceUUID, target)
}

func newNamedMCPTestHandler(t *testing.T, serverName, targetURL string) (*Handler, string, string) {
	t.Helper()
	snapshot, err := json.Marshal(map[string]any{"mcp_servers": []any{map[string]any{
		"name": serverName, "type": "http", "url": targetURL,
	}}})
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
	identity := SessionCredentialIdentity{
		SessionID: "cse_test", PublicSessionID: "sesn_test", AgentID: "agent_test", AgentVersion: 1,
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
	}
	mcpToken, err := credentials.IssueMCPProxy(identity)
	if err != nil {
		t.Fatalf("issue MCP proxy token: %v", err)
	}
	sessionToken, err := credentials.Issue(identity)
	if err != nil {
		t.Fatalf("issue session ingress token: %v", err)
	}
	handler := NewHandler(config.Config{
		Tunnel: config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"},
	}, NewServiceWithCredentials(nil, credentials, nil), nil, slog.Default())
	handler.loadMCPPolicyContext = func(context.Context, upstreamProxyIdentity) (mcpProxyPolicyContext, error) {
		return mcpProxyPolicyContext{policy: policy}, nil
	}
	return handler, mcpToken, sessionToken
}

func TestMCPProxyVaultInjectionRejectedReturns502(t *testing.T) {
	targetURL := "https://mcp.example.com/mcp"
	handler, token := newMCPProxyTestHandler(t, targetURL, slog.Default())
	handler.wrapMCPVaultTransport = func(context.Context, SessionCredentialClaims, *url.URL, http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, vaults.ErrInjectionRejected
		})
	}

	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	proxyServer := httptest.NewServer(router)
	t.Cleanup(proxyServer.Close)

	request, err := http.NewRequest(http.MethodPost, proxyServer.URL+"/v2/ccr-sessions/cse_test/mcp?"+url.Values{"mcp_url": {targetURL}}.Encode(), bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", response.StatusCode)
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
	var tunnelCandidate *url.URL
	handler.tunnelInvoker = tunnelInvokerFuncs{serve: func(_ http.ResponseWriter, _ *http.Request, _, _ string, target *url.URL) bool {
		tunnelCandidate = target
		return false
	}}
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
	handler.wrapMCPVaultTransport = func(_ context.Context, claims SessionCredentialClaims, target *url.URL, base http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if claims.SessionID != "cse_test" || target.String() != targetURL {
				t.Errorf("credential injector context = session %q target %q", claims.SessionID, target)
			}
			out := req.Clone(req.Context())
			out.Header.Set("Authorization", "Bearer upstream-secret")
			out.Header.Set("X-MCP-Key", "vault-secret")
			return base.RoundTrip(out)
		})
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
	if tunnelCandidate == nil || tunnelCandidate.String() != targetURL {
		t.Fatalf("legacy query-based MCP Tunnel candidate = %#v", tunnelCandidate)
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
func newMCPProxyTestHandler(t *testing.T, allowedMCPURL string, logger *slog.Logger) (*Handler, string) {
	t.Helper()
	snapshot, err := json.Marshal(map[string]any{"mcp_servers": []any{map[string]any{"type": "http", "url": allowedMCPURL}}})
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
	handler := NewHandler(config.Config{}, NewServiceWithCredentials(nil, credentials, nil), nil, logger)
	handler.loadMCPPolicyContext = func(context.Context, upstreamProxyIdentity) (mcpProxyPolicyContext, error) {
		return mcpProxyPolicyContext{policy: policy}, nil
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
	return handler, token
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

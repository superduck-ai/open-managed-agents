package codesessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestDSHCapabilityRoutesRequireProtocolAndRejectLegacyCredentials(t *testing.T) {
	handler := NewHandler(config.Config{}, newTestService(t, nil), nil, nil).WithImageAttachments(nil)
	identity := testSessionCredentialIdentity()
	identity.WorkerEpoch = 7
	token, err := handler.service.credentials.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Route("/v1", handler.RegisterV1Routes)
	for _, path := range []string{"/sandbox/mcp", "/mcp", "/web/search", "/web/fetch", "/otlp/v1/logs", "/otlp/v1/traces"} {
		request := httptest.NewRequest("POST", "/v1/dsr/sessions/"+identity.SessionID+path, strings.NewReader(`{}`))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 || response.Header().Get("DSR-Error") != "unsupported_protocol" {
			t.Fatalf("path=%s response=%d %s", path, response.Code, response.Body.String())
		}
		request = httptest.NewRequest("POST", "/v1/dsr/sessions/"+identity.SessionID+path, strings.NewReader(`{}`))
		request.Header.Set("DSR-Protocol", "1")
		request.Header.Set("Authorization", "Bearer "+token)
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("legacy token path=%s response=%d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestDSHMCPURLsAndHeadersPreserveProtocolIsolation(t *testing.T) {
	identity := runtimeMCPTestIdentity()
	identity.Protocol = "dsr1"
	encoded, err := BuildMCPRuntimeConfig(runtimeMCPTestSource("sse"), identity, runtimeMCPTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, server := range result.MCPServers {
		if strings.Contains(server.URL, "/v1/dsr/sessions/") {
			found = true
			if server.Headers["DSR-Protocol"] != "1" || server.Headers["Authorization"] == "" {
				t.Fatalf("server=%+v", server)
			}
		}
	}
	if !found {
		t.Fatalf("no DSR tunnel URL: %s", encoded)
	}
	request := httptest.NewRequest("GET", "https://oma.example/v1/dsr/sessions/cse_root/mcp/server", nil)
	if value := runtimeMCPResourceURL(request, "cse_root", "server"); value != "https://oma.example/v1/dsr/sessions/cse_root/mcp/server" {
		t.Fatal(value)
	}
	if value := runtimeMCPMetadataURL(request, "cse_root", "server"); value != "https://oma.example/.well-known/oauth-protected-resource/v1/dsr/sessions/cse_root/mcp/server" {
		t.Fatal(value)
	}
	headers := make(http.Header)
	for name, value := range map[string]string{"Authorization": "private", "DSR-Protocol": "1", "DSR-Thread-ID": "child", runtimeThreadHeader: "child", "Mcp-Session-Id": "mcp-session"} {
		headers.Set(name, value)
	}
	filtered := sanitizedMCPProxyHeaders(headers)
	for _, name := range []string{"Authorization", "DSR-Protocol", "DSR-Thread-ID", runtimeThreadHeader} {
		if filtered.Get(name) != "" {
			t.Fatalf("leaked %s", name)
		}
	}
	if filtered.Get("Mcp-Session-Id") != "mcp-session" {
		t.Fatal("dropped native MCP identity")
	}
}

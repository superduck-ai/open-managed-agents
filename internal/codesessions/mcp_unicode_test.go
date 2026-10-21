package codesessions

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/mcpnames"
)

func TestMCPUnicodeRuntimeRejectsAliasCollisions(t *testing.T) {
	alias := mcpnames.ServerKey("Peer-测试")
	source, _ := json.Marshal(map[string]any{"mcp_servers": []map[string]string{
		{"name": "Peer-测试", "type": "url", "url": "https://example.com/mcp"},
		{"name": alias, "type": "url", "url": "https://other.example/mcp"},
	}})
	if _, err := BuildMCPRuntimeConfig(source, MCPRuntimeIdentity{}, config.TunnelConfig{}); !errors.Is(err, ErrMCPDeclarationInvalid) {
		t.Fatalf("alias collision accepted: %v", err)
	}
}

func TestMCPUnicodeRuntimeAndPermissionsKeepOriginalIdentity(t *testing.T) {
	const name = "Peer-测试"
	prefix := mcpnames.ToolPrefix(name)
	for _, enabled := range []bool{false, true} {
		source, _ := json.Marshal(map[string]any{
			"mcp_servers": []map[string]string{{"name": name, "type": "url", "url": "https://example.com/mcp"}},
			"tools":       []map[string]any{{"type": "mcp_toolset", "mcp_server_name": name, "default_config": map[string]any{"enabled": enabled, "permission_policy": map[string]string{"type": "always_allow"}}}},
		})
		wire, err := BuildMCPRuntimeConfig(source, MCPRuntimeIdentity{}, config.TunnelConfig{})
		if err != nil {
			t.Fatal(err)
		}
		var runtime mcpRuntimeDocument
		if err := json.Unmarshal(wire, &runtime); err != nil || len(runtime.Servers[prefix]) == 0 {
			t.Fatalf("Unicode server not mapped: %s %v", wire, err)
		}
		permission, identity := resolveToolPermissionFromAgentSnapshot(source, "mcp__"+prefix+"__fetch")
		want := resolvedToolPermissionDeny
		if enabled {
			want = resolvedToolPermissionAllow
		}
		if permission != want || identity.ServerName != name || identity.ToolName != "fetch" {
			t.Fatalf("incorrect permission mapping: %s %+v", permission, identity)
		}
		var snapshot toolPermissionSnapshot
		if err := json.Unmarshal(source, &snapshot); err != nil {
			t.Fatal(err)
		}
		snapshot.MCPServers = append(snapshot.MCPServers, mcpServerDeclaration{Name: prefix})
		if _, found := resolveMCPToolIdentity(snapshot, "mcp__"+prefix+"__fetch"); found {
			t.Fatal("ambiguous alias resolved to a server")
		}
	}
}

func TestNamedMCPProxyPreservesUnicodeAndPercentNames(t *testing.T) {
	for _, name := range []string{"Peer-测试", "测试%41", "测试/工具", " peer "} {
		targetURL := "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"
		handler, token := newNamedMCPTestHandler(t, name, targetURL)
		handler.tunnelInvoker = tunnelInvokerFuncs{serve: func(w http.ResponseWriter, _ *http.Request, _, _ string, _ *url.URL) bool {
			w.WriteHeader(http.StatusNoContent)
			return true
		}}
		router := chi.NewRouter()
		router.Route("/v2", handler.RegisterV2Routes)
		request := httptest.NewRequest(http.MethodPost, "/v2/ccr-sessions/cse_test/mcp/"+url.PathEscape(name), strings.NewReader(`{}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("name %q status = %d: %s", name, response.Code, response.Body.String())
		}
	}
}

func TestMCPUnicodePublicEventUsesOriginalServerName(t *testing.T) {
	const name = "Peer-测试"
	_, wire, err := toolCallPublicPayload("cse_unicode", toolPermissionRequest{
		ToolName: "mcp__" + mcpnames.ToolPrefix(name) + "__fetch", ToolUseID: "tool_unicode",
	}, toolIdentity{Kind: "mcp", ServerName: name, ToolName: "fetch"}, resolvedToolPermissionAllow, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Type       string `json:"type"`
		Name       string `json:"name"`
		ServerName string `json:"mcp_server_name"`
	}
	if err := json.Unmarshal(wire, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "agent.mcp_tool_use" || event.Name != "fetch" || event.ServerName != name {
		t.Fatalf("public identity lost: %s", wire)
	}
}

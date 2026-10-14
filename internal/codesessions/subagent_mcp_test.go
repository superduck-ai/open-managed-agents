package codesessions

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNamedMCPGatewayUsesSessionScope(t *testing.T) {
	target := "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"
	handler, token := newNamedMCPTestHandler(t, "local", target)
	snapshot := json.RawMessage(`{"mcp_servers":[{"name":"local","type":"url","url":"` + target + `"}],"multiagent":{"agents":[{"name":"child","mcp_servers":[{"name":"child_only","type":"url","url":"https://child.example/mcp"}]}]}}`)
	handler.loadMCPPolicyContext = func(_ context.Context, _ upstreamProxyIdentity) (mcpProxyPolicyContext, error) {
		policy, err := networkpolicy.ParseMCPProxyPolicy([]byte(`{"type":"cloud","networking":{"type":"unrestricted"}}`), snapshot)
		return mcpProxyPolicyContext{policy: policy}, err
	}
	var calls int
	handler.tunnelInvoker = tunnelInvokerFuncs{serve: func(w http.ResponseWriter, r *http.Request, _, _ string, actual *url.URL) bool {
		calls++
		if actual.String() != target || r.Header.Get("Authorization") != "" {
			t.Fatal("incorrect child Tunnel target or leaked ingress credential")
		}
		w.WriteHeader(http.StatusNoContent)
		return true
	}}
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	for _, query := range []string{"", "config_agent_name=unknown&config_agent_id=agent_foreign"} {
		request := httptest.NewRequest("POST", "/v2/ccr-sessions/cse_test/mcp/local?"+query, strings.NewReader(`{}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 204 {
			t.Fatalf("query=%q status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest("POST", "/v2/ccr-sessions/cse_test/mcp/child_only", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatalf("child configuration changed session MCP servers: %d", response.Code)
	}
	if calls != 2 {
		t.Fatalf("Tunnel called %d times, want two", calls)
	}
	request = httptest.NewRequest("GET", "https://runtime.example/metadata?config_agent_name=child&config_agent_id=agent_child", nil)
	for _, address := range []string{runtimeMCPMetadataURL(request, "cse_test", "local"), runtimeMCPResourceURL(request, "cse_test", "local")} {
		parsed, err := url.Parse(address)
		if err != nil || parsed.RawQuery != "" {
			t.Fatalf("MCP discovery retained child scope: %s, error=%v", address, err)
		}
	}
}

package codesessions

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNamedMCPGatewayUsesPinnedChildScope(t *testing.T) {
	target := "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"
	handler, token := newNamedMCPTestHandler(t, "local", target)
	snapshot := json.RawMessage(`{"mcp_servers":[],"multiagent":{"agents":[{"id":"agent_child","name":"计算Agent","version":2,"mcp_servers":[{"name":"local","type":"url","url":"` + target + `"}]}]}}`)
	handler.loadMCPPolicyContext = func(_ context.Context, identity upstreamProxyIdentity) (mcpProxyPolicyContext, error) {
		effective := snapshot
		var err error
		if identity.configAgentName != "" {
			effective, err = agentsnapshot.ForSubagentType(snapshot, identity.configAgentName)
			if err != nil {
				return mcpProxyPolicyContext{}, err
			}
		} else if identity.configAgentID != "" {
			effective, err = agentsnapshot.ForSubagent(snapshot, identity.configAgentID)
			if err != nil {
				return mcpProxyPolicyContext{}, err
			}
		}
		policy, err := networkpolicy.ParseMCPProxyPolicy([]byte(`{"type":"cloud","networking":{"type":"unrestricted"}}`), effective)
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
	for _, test := range []struct {
		query  string
		status int
	}{{"config_agent_name=unknown", 403}, {"config_agent_id=agent_foreign", 403}, {"", 404}, {"config_agent_name=" + url.QueryEscape("计算Agent"), 204}, {"config_agent_id=agent_child", 204}} {
		request := httptest.NewRequest("POST", "/v2/ccr-sessions/cse_test/mcp/local?"+test.query, strings.NewReader(`{}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("query=%q status=%d want=%d body=%s", test.query, response.Code, test.status, response.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("Tunnel called %d times, want two", calls)
	}
	request := httptest.NewRequest("GET", "https://runtime.example/metadata?config_agent_id=agent_child", nil)
	if !strings.HasSuffix(runtimeMCPMetadataURL(request, "cse_test", "local"), "?config_agent_id=agent_child") || !strings.HasSuffix(runtimeMCPResourceURL(request, "cse_test", "local"), "?config_agent_id=agent_child") {
		t.Fatal("MCP OAuth discovery lost child scope")
	}
	request = httptest.NewRequest("GET", "https://runtime.example/metadata?config_agent_name="+url.QueryEscape("计算Agent"), nil)
	for _, address := range []string{runtimeMCPMetadataURL(request, "cse_test", "local"), runtimeMCPResourceURL(request, "cse_test", "local")} {
		parsed, err := url.Parse(address)
		if err != nil || parsed.Query().Get("config_agent_name") != "计算Agent" || parsed.Query().Get("config_agent_id") != "" {
			t.Fatalf("name scope lost: %s, error=%v", address, err)
		}
	}
}

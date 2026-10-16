package agents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const referenceTestServers = `[{"name":"peer","type":"url","url":"https://example.com/mcp"}]`
const referenceTestTools = `[{"type":"mcp_toolset","mcp_server_name":"peer"}]`

func TestNormalizeToolsRequiresEveryMCPServerReference(t *testing.T) {
	for _, tools := range []string{`[]`, `null`, `[{"type":"agent_toolset_20260401"}]`} {
		t.Run(tools, func(t *testing.T) {
			if _, err := normalizeTools(json.RawMessage(tools), json.RawMessage(referenceTestServers)); err == nil {
				t.Fatal("accepted MCP server without a matching toolset")
			}
		})
	}
}

func TestAgentUpdateValidatesMergedMCPReferences(t *testing.T) {
	for _, test := range []struct {
		name           string
		currentServers string
		currentTools   string
		update         string
	}{
		{"clear tools empty", referenceTestServers, referenceTestTools, `{"tools":[]}`},
		{"clear tools null", referenceTestServers, referenceTestTools, `{"tools":null}`},
		{"remove server empty", referenceTestServers, referenceTestTools, `{"mcp_servers":[]}`},
		{"remove server null", referenceTestServers, referenceTestTools, `{"mcp_servers":null}`},
		{"add server without tools", `[]`, `[]`, `{"mcp_servers":` + referenceTestServers + `}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := db.Agent{MCPServers: json.RawMessage(test.currentServers), Tools: json.RawMessage(test.currentTools)}
			var body agentMutationRequest
			if err := json.Unmarshal([]byte(test.update), &body); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/agents/agent", nil)
			if _, err := (&Handler{}).stateFromUpdate(request, auth.Principal{}, current, &body); err == nil {
				t.Fatal("accepted merged Agent configuration with broken MCP references")
			}
		})
	}
}

func TestMCPReferencesPermitMatchingToolsetsAndClearingBothFields(t *testing.T) {
	if _, err := normalizeTools(json.RawMessage(referenceTestTools), json.RawMessage(referenceTestServers)); err != nil {
		t.Fatal(err)
	}
	for _, empty := range []string{`[]`, `null`} {
		tools, err := normalizeTools(json.RawMessage(empty), json.RawMessage(`[]`))
		if err != nil || string(tools) != `[]` {
			t.Fatalf("empty tools = %s, error = %v", tools, err)
		}
		body := agentMutationRequest{Tools: json.RawMessage(empty), MCPServers: json.RawMessage(empty)}
		current := db.Agent{MCPServers: json.RawMessage(referenceTestServers), Tools: json.RawMessage(referenceTestTools)}
		state, err := (&Handler{}).stateFromUpdate(httptest.NewRequest(http.MethodPost, "/", nil), auth.Principal{}, current, &body)
		if err != nil || string(state.Tools) != `[]` || string(state.MCPServers) != `[]` {
			t.Fatalf("cleared state = %+v, error = %v", state, err)
		}
	}
}

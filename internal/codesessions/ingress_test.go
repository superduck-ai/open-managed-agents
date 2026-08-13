package codesessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

func TestCodeSessionHTTPPollReturnsErrorsThroughAdapter(t *testing.T) {
	handler := NewHandler(config.Config{}, newTestService(t, nil), nil, nil)
	router := chi.NewRouter()
	handler.RegisterV1Routes(router)

	request := httptest.NewRequest(http.MethodGet, "/code/sessions/cse_test/", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestSessionContextFromCodeSessionUsesStoredConfig(t *testing.T) {
	record := db.CodeSession{
		WorkDir: "/workspace/repo",
		Model:   "claude-opus-4-8",
		Metadata: json.RawMessage(`{
			"config":{
				"outcomes":[{"type":"complete"}],
				"custom_system_prompt":"You are Codex.",
				"append_system_prompt":"Use MCP when useful.",
				"mcp_config":{"mcpServers":{"notion":{"type":"http","url":"https://mcp.notion.com/mcp"}}}
			}
		}`),
	}

	context := sessionContextFromCodeSession(record)
	if context["cwd"] != "/workspace/repo" || context["model"] != "claude-opus-4-8" {
		t.Fatalf("unexpected base context: %#v", context)
	}
	if context["custom_system_prompt"] != "You are Codex." || context["append_system_prompt"] != "Use MCP when useful." {
		t.Fatalf("unexpected prompts: %#v", context)
	}
	if len(context["outcomes"].([]any)) != 1 {
		t.Fatalf("unexpected outcomes: %#v", context["outcomes"])
	}
	mcpConfig := context["mcp_config"].(map[string]any)
	servers := mcpConfig["mcpServers"].(map[string]any)
	notion := servers["notion"].(map[string]any)
	if notion["type"] != "http" || notion["url"] != "https://mcp.notion.com/mcp" {
		t.Fatalf("unexpected mcp config: %#v", mcpConfig)
	}
}

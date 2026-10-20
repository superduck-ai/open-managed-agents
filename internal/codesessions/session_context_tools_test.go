package codesessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCurrentToolSnapshotReplacesFrozenMCPContext(t *testing.T) {
	original := db.CodeSession{Metadata: json.RawMessage(`{"owner":"retained","config":{"system_prompt":"fixed","mcp_config":{"mcpServers":{"old":{"type":"http","url":"https://old.example/mcp"}}},"tools":[{"type":"bash_20260401"}]}}`)}
	updated, err := codeSessionWithToolSnapshot(original, json.RawMessage(`{"tools":[{"type":"mcp_toolset","mcp_server_name":"peer"}],"mcp_servers":[{"type":"url","name":"peer","url":"http://new.example/mcp"}],"system":"must not replace fixed prompt"}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := sessionContextFromCodeSession(updated, MCPRuntimeIdentity{}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var mcp struct {
		Servers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(result["mcp_config"].(json.RawMessage), &mcp) != nil || len(mcp.Servers) != 1 || mcp.Servers["peer"].URL != "http://new.example/mcp" {
		t.Fatal("current MCP URL did not replace frozen client config")
	}
	if result["system_prompt"] != "fixed" {
		t.Fatal("mutable tools changed immutable prompt")
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(original.Metadata, &before)
	_ = json.Unmarshal(updated.Metadata, &after)
	if string(before["owner"]) != string(after["owner"]) || !json.Valid(original.Metadata) || string(original.Metadata) == string(updated.Metadata) {
		t.Fatal("context update mutated or discarded stored metadata")
	}
}

func TestCurrentToolSnapshotClearsRemovedServersAndRejectsInvalidJSON(t *testing.T) {
	record := db.CodeSession{Metadata: json.RawMessage(`{"config":{"mcp_config":{"mcpServers":{"old":{"type":"http","url":"https://old.example/mcp"}}}}}`)}
	updated, err := codeSessionWithToolSnapshot(record, json.RawMessage(`{"tools":[],"mcp_servers":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := sessionContextFromCodeSession(updated, MCPRuntimeIdentity{}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if string(result["mcp_config"].(json.RawMessage)) != "{}" {
		t.Fatal("removed server remained in context")
	}
	if _, err := codeSessionWithToolSnapshot(record, json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid source silently reused stale tools")
	}
}

func TestWorkerToolRefreshCannotBeSuppressedByClient(t *testing.T) {
	payload, err := workerPayloadWithSystemContext(json.RawMessage(`{"type":"user.message","oma_refresh_tools":false}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Refresh bool `json:"oma_refresh_tools"`
	}
	if json.Unmarshal(payload, &result) != nil || !result.Refresh {
		t.Fatal("client prevented authoritative runtime tool refresh")
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`[]`)} {
		if _, err := codeSessionWithToolSnapshot(db.CodeSession{}, raw); err == nil {
			t.Fatal("non-object agent snapshot accepted")
		}
	}
}

func TestCurrentToolSnapshotScopesLocalPermissionCache(t *testing.T) {
	record := db.CodeSession{Metadata: json.RawMessage(`{"config":{"tools":[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"always_allow"}}}]}}`)}
	scoped, err := codeSessionWithToolSnapshot(record, json.RawMessage(`{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true,"permission_policy":{"type":"always_ask"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := sessionContextFromCodeSession(scoped, MCPRuntimeIdentity{}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	policies := result["runtime_tool_permissions"].(map[string]string)
	if policies["read"] != "ask" || policies["bash"] != "deny" {
		t.Fatalf("scoped policies = %v", policies)
	}
}

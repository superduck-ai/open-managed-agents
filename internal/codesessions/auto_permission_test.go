package codesessions

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAutoPermissionIndeterminateRequiresConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, snapshot, tool string
		want                 resolvedToolPermission
		auto                 bool
	}{
		{"disabled", `{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","enabled":false,"permission_policy":{"type":"auto"}}]}]}`, "Write", resolvedToolPermissionDeny, false},
		{"unknown", `{"tools":[]}`, "unknown", resolvedToolPermissionDeny, false},
		{"explicit override", `{"tools":[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"auto"}},"configs":[{"name":"write","permission_policy":{"type":"always_allow"}}]}]}`, "Write", resolvedToolPermissionAllow, false},
		{"auto override", `{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","permission_policy":{"type":"auto"}}]}]}`, "Write", resolvedToolPermissionAsk, true},
		{"auto default", `{"tools":[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"auto"}}}]}`, "Write", resolvedToolPermissionAsk, true},
		{"MCP", `{"mcp_servers":[{"name":"weather"}],"tools":[{"type":"mcp_toolset","mcp_server_name":"weather","default_config":{"permission_policy":{"type":"auto"}}}]}`, "mcp__weather__read", resolvedToolPermissionAsk, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			permission, identity := resolveToolPermissionFromAgentSnapshot(json.RawMessage(tc.snapshot), tc.tool)
			if permission != tc.want {
				t.Fatalf("permission=%s", permission)
			}
			_, raw, err := toolCallPublicPayload("code", toolPermissionRequest{ToolName: tc.tool, ToolUseID: "call", RequestID: "request"}, identity, permission, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Permission string `json:"evaluated_permission"`
				Evaluation struct {
					Type       string `json:"type"`
					Permission struct {
						Type   string `json:"type"`
						Reason string `json:"reason_code"`
					} `json:"evaluated_permission"`
				} `json:"evaluation"`
			}
			if json.Unmarshal(raw, &event) != nil {
				t.Fatal(string(raw))
			}
			if (event.Evaluation.Type == "auto") != tc.auto {
				t.Fatalf("%s", raw)
			}
			if tc.auto && (event.Permission != "ask" || event.Evaluation.Permission.Type != "ask" || event.Evaluation.Permission.Reason != "indeterminate") {
				t.Fatalf("%s", raw)
			}
		})
	}
}

func TestAutoMCPRuntimeRequestsApproval(t *testing.T) {
	var declarations []mcpToolsetDeclaration
	if err := json.Unmarshal([]byte(`[{"type":"mcp_toolset","mcp_server_name":"weather","configs":[{"name":"read","permission_policy":{"type":"auto"}}]}]`), &declarations); err != nil {
		t.Fatal(err)
	}
	tools := mcpRuntimeToolsets(declarations)
	if len(tools["weather"]) != 1 || tools["weather"][0].PermissionPolicy != "always_ask" {
		t.Fatal(tools)
	}
}

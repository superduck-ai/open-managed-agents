package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAutoPermissionPolicyNormalization(t *testing.T) {
	for _, policy := range []string{"", "allow", "unknown"} {
		if _, err := normalizePermissionPolicy(map[string]any{"type": policy}, "always_allow"); err == nil {
			t.Fatalf("invalid policy accepted: %s", policy)
		}
	}
	for _, toolset := range []string{
		`[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"auto"}},"configs":[{"name":"write","permission_policy":{"type":"auto"}}]}]`,
		`[{"type":"mcp_toolset","mcp_server_name":"weather","default_config":{"permission_policy":{"type":"auto"}},"configs":[{"name":"read","permission_policy":{"type":"auto"}}]}]`,
	} {
		servers := json.RawMessage(`[]`)
		if strings.Contains(toolset, "mcp_toolset") {
			servers = json.RawMessage(`[{"name":"weather","url":"https://example.com/mcp"}]`)
		}
		normalized, err := normalizeTools(json.RawMessage(toolset), servers)
		if err != nil {
			t.Fatal(err)
		}
		var tools []struct {
			Default struct {
				Policy struct {
					Type string `json:"type"`
				} `json:"permission_policy"`
			} `json:"default_config"`
			Configs []struct {
				Policy struct {
					Type string `json:"type"`
				} `json:"permission_policy"`
			} `json:"configs"`
		}
		if err = json.Unmarshal(normalized, &tools); err != nil || len(tools) != 1 || tools[0].Default.Policy.Type != "auto" || tools[0].Configs[0].Policy.Type != "auto" {
			t.Fatalf("%s %v", normalized, err)
		}
	}
}

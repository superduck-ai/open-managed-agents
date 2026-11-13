package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPortToolConfigRejectsInvalidPolicy(t *testing.T) {
	tools := `[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url","permission_policy":{"type":"allow"}}]}]`
	if _, err := normalizeTools(json.RawMessage(tools), json.RawMessage(`[]`)); err == nil {
		t.Fatal("invalid get_port_url policy accepted")
	}
	tools = `[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url"},{"name":"get_port_url"}]}]`
	if _, err := normalizeTools(json.RawMessage(tools), json.RawMessage(`[]`)); err == nil {
		t.Fatal("duplicate get_port_url config accepted")
	}
}

func TestToolsWithoutPortToolNormalizeAsBefore(t *testing.T) {
	for _, testCase := range []struct{ input, want string }{
		{
			`[{"type":"agent_toolset_20260401"}]`,
			`[{"configs":[{"enabled":false,"name":"ask_user_question","permission_policy":{"type":"always_allow"},"type":"ask_user_question"}],"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}},"type":"agent_toolset_20260401"}]`,
		},
		{
			`[{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}},"configs":[{"name":"bash","enabled":false},{"name":"ask_user_question","enabled":true}]}]`,
			`[{"configs":[{"enabled":false,"name":"bash","permission_policy":{"type":"always_ask"},"type":"bash"},{"enabled":true,"name":"ask_user_question","permission_policy":{"type":"always_ask"},"type":"ask_user_question"}],"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}},"type":"agent_toolset_20260401"}]`,
		},
		{
			`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"web_fetch","allowed_domains":["example.com"]}]}]`,
			`[{"configs":[{"allowed_domains":["example.com"],"enabled":true,"name":"web_fetch","permission_policy":{"type":"always_allow"},"type":"web_fetch"},{"enabled":false,"name":"ask_user_question","permission_policy":{"type":"always_allow"},"type":"ask_user_question"}],"default_config":{"enabled":false,"permission_policy":{"type":"always_allow"}},"type":"agent_toolset_20260401"}]`,
		},
	} {
		normalized, err := normalizeTools(json.RawMessage(testCase.input), json.RawMessage(`[]`))
		if err != nil {
			t.Fatal(err)
		}
		if string(normalized) != testCase.want {
			t.Fatalf("normalized tools changed\n got %s\nwant %s", normalized, testCase.want)
		}
	}
}

func TestPortToolConfigFollowsToolsetDefaultPolicy(t *testing.T) {
	for defaultConfig, want := range map[string]string{
		`{"permission_policy":{"type":"always_ask"}}`: `{"enabled":true,"name":"get_port_url","permission_policy":{"type":"always_ask"},"type":"get_port_url"}`,
		`{}`: `{"enabled":true,"name":"get_port_url","permission_policy":{"type":"always_allow"},"type":"get_port_url"}`,
	} {
		tools := `[{"type":"agent_toolset_20260401","default_config":` + defaultConfig + `,"configs":[{"name":"get_port_url"}]}]`
		normalized, err := normalizeTools(json.RawMessage(tools), json.RawMessage(`[]`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(normalized), want) {
			t.Fatalf("get_port_url config = %s", normalized)
		}
	}
	tools := `[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url","permission_policy":{"type":"always_ask"}}]}]`
	normalized, err := normalizeTools(json.RawMessage(tools), json.RawMessage(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normalized), `"name":"get_port_url","permission_policy":{"type":"always_ask"}`) {
		t.Fatalf("explicit get_port_url policy lost: %s", normalized)
	}
}

func TestMCPToolNamedGetPortURLKeepsToolsetDefaultPolicy(t *testing.T) {
	tools := `[{"type":"mcp_toolset","mcp_server_name":"docs","default_config":{"permission_policy":{"type":"always_ask"}},"configs":[{"name":"get_port_url"}]}]`
	servers := `[{"name":"docs","url":"https://example.com/mcp"}]`
	normalized, err := normalizeTools(json.RawMessage(tools), json.RawMessage(servers))
	if err != nil {
		t.Fatal(err)
	}
	var toolsets []struct {
		Configs []struct {
			Name   string `json:"name"`
			Policy struct {
				Type string `json:"type"`
			} `json:"permission_policy"`
		} `json:"configs"`
	}
	if err := json.Unmarshal(normalized, &toolsets); err != nil || len(toolsets) != 1 || len(toolsets[0].Configs) != 1 {
		t.Fatalf("%s %v", normalized, err)
	}
	if config := toolsets[0].Configs[0]; config.Name != "get_port_url" || config.Policy.Type != "always_ask" {
		t.Fatalf("mcp get_port_url config = %s", normalized)
	}
}

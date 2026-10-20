package agents

import (
	"encoding/json"
	"testing"
)

func TestSessionToolsRejectInvalidAndUnmatchedMCP(t *testing.T) {
	for _, raw := range []string{`{}`, `true`, `[{"type":"future"}]`, `[{"type":"agent_toolset_20260401","configs":[{"name":"future"}]}]`, `[{"type":"mcp_toolset","mcp_server_name":"missing"}]`} {
		if _, err := NormalizeSessionToolsOverride(json.RawMessage(raw), json.RawMessage(`[]`), nil); err == nil {
			t.Fatal("invalid effective tools accepted", raw)
		}
	}
}

func TestSessionToolsRequireEffectiveReadForSkills(t *testing.T) {
	skills := json.RawMessage(`[{"id":"skill_fixture","version":1}]`)
	for _, raw := range []string{`null`, `[]`, `[{"type":"agent_toolset_20260401","default_config":{"enabled":false}}]`, `[{"type":"agent_toolset_20260401","default_config":{"enabled":true},"configs":[{"name":"read","enabled":false}]}]`} {
		if _, err := NormalizeSessionToolsOverride(json.RawMessage(raw), json.RawMessage(`[]`), skills); err == nil {
			t.Fatal("skills without enabled Read accepted", raw)
		}
	}
	for _, raw := range []string{`[{"type":"agent_toolset_20260401","default_config":{"enabled":true}}]`, `[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]`} {
		if _, err := NormalizeSessionToolsOverride(json.RawMessage(raw), json.RawMessage(`[]`), skills); err != nil {
			t.Fatal("effective Read rejected", raw, err)
		}
	}
	if sessionToolsEnableRead(json.RawMessage(`[{"type":"custom","name":"read"}]`)) {
		t.Fatal("custom name cannot substitute builtin Read")
	}
}

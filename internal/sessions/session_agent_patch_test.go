package sessions

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSessionAgentPatchRejectsReadonlyAndUnknownBeforeChanges(t *testing.T) {
	current := json.RawMessage(`{"id":"agent_source","version":3,"model":{"id":"model"},"system":"source","tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":true}}],"mcp_servers":[],"skills":[],"multiagent":{"type":"multiagent_20261001"},"opaque":{"number":9007199254740993}}`)
	original := bytes.Clone(current)
	for _, patch := range []string{
		`{"multiagent":{"type":"multiagent_20261001"}}`,
		`{"multiagent":null,"tools":[]}`,
		`{"model":{"id":"other"}}`,
		`{"system":null}`,
		`{"skills":[]}`,
		`{"id":"other"}`,
		`{"version":4}`,
		`{"type":"agent"}`,
		`{"name":"other"}`,
		`{"description":"other"}`,
		`{"metadata":{}}`,
		`{"future":null}`,
		`{"TOOLS":[]}`,
		`null`, `[]`, `"text"`,
	} {
		t.Run(patch, func(t *testing.T) {
			result, err := patchSessionAgent(current, json.RawMessage(patch))
			if err == nil || result != nil {
				t.Fatalf("readonly/invalid patch accepted: result=%s error=%v", result, err)
			}
			if !bytes.Equal(current, original) {
				t.Fatal("rejected patch mutated source input")
			}
		})
	}
}

func TestSessionAgentPatchValidFinalToolsAndMCPConfiguration(t *testing.T) {
	current := json.RawMessage(`{"id":"agent_source","version":3,"model":{"id":"model"},"system":"source","tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":true}}],"mcp_servers":[],"skills":[],"multiagent":{"type":"multiagent_20261001"},"opaque":{"number":9007199254740993}}`)
	for _, patch := range []string{`{}`, `{"tools":null}`, `{"tools":[]}`, `{"mcp_servers":[]}`, `{"tools":[{"type":"mcp_toolset","mcp_server_name":"peer"}],"mcp_servers":[{"type":"url","name":"peer","url":"https://peer.example/mcp"}]}`} {
		result, err := patchSessionAgent(current, json.RawMessage(patch))
		if err != nil {
			t.Fatal(patch, err)
		}
		var before, after map[string]json.RawMessage
		json.Unmarshal(current, &before)
		json.Unmarshal(result, &after)
		for _, field := range []string{"id", "version", "model", "system", "multiagent", "opaque", "skills"} {
			if !bytes.Equal(before[field], after[field]) {
				t.Fatal("immutable/opaque snapshot changed", field, string(after[field]))
			}
		}
	}
	for _, patch := range []string{`{"tools":{}}`, `{"tools":"wrong"}`, `{"mcp_servers":{}}`, `{"mcp_servers":[{"type":"url","name":"peer","url":"https://peer.example/mcp"}]}`} {
		if _, err := patchSessionAgent(current, json.RawMessage(patch)); err == nil {
			t.Fatal("invalid final tools/MCP state accepted", patch)
		}
	}
	withSkills := json.RawMessage(`{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":true}}],"mcp_servers":[],"skills":[{"type":"custom","skill_id":"skill_fixture","version":"v1"}]}`)
	if _, err := patchSessionAgent(withSkills, json.RawMessage(`{"tools":null}`)); err == nil {
		t.Fatal("Read removed from nonempty effective skills")
	}
}

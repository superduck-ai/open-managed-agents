package sessions

import (
	"encoding/json"
	"testing"
)

func TestInlineThreadSnapshotRejectsInvalidParent(t *testing.T) {
	if _, err := sessionThreadAgentSnapshot(map[string]any{"agent": map[string]any{"type": "inline", "name": "worker"}}, json.RawMessage(`invalid`)); err == nil {
		t.Fatal("invalid parent accepted")
	}
}

func TestInlineThreadSnapshotInheritsConfigurationWithoutResourceIdentity(t *testing.T) {
	parent := json.RawMessage(`{"type":"agent","id":"agent_parent","version":3,"name":"parent","description":"parent description","system":"parent system","model":{"id":"model-fixture"},"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false}}],"mcp_servers":[],"skills":[],"metadata":{"fixture":"snapshot"},"multiagent":{"type":"multiagent_20261001"}}`)
	raw, err := sessionThreadAgentSnapshot(map[string]any{"agent": map[string]any{"type": "inline", "name": "worker", "system": " task system "}}, parent)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "inline" || got["name"] != "worker" || got["system"] != " task system " || got["description"] != nil {
		t.Fatalf("wrong inline identity: %s", raw)
	}
	for _, key := range []string{"id", "version", "multiagent"} {
		if _, exists := got[key]; exists {
			t.Fatalf("inherited %s: %s", key, raw)
		}
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(parent, &source); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"model", "tools", "mcp_servers", "skills", "metadata"} {
		encoded, _ := json.Marshal(got[key])
		var expected any
		_ = json.Unmarshal(source[key], &expected)
		want, _ := json.Marshal(expected)
		if string(encoded) != string(want) {
			t.Fatalf("%s lost: %s", key, raw)
		}
	}
	saved, err := sessionThreadAgentSnapshot(map[string]any{"agent": map[string]any{"type": "agent", "id": "agent_saved", "version": 2}}, parent)
	if err != nil || string(saved) != `{"id":"agent_saved","type":"agent","version":2}` {
		t.Fatalf("saved agent changed: %s %v", saved, err)
	}
}

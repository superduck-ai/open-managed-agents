package codesessions

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTrustedDSHInlineSnapshotHasNoSavedIdentityOrDelegation(t *testing.T) {
	parent := json.RawMessage(`{"type":"agent","id":"parent","version":4,"description":"saved","name":"parent","system":"parent system","model":{"id":"model"},"tools":[{"type":"agent_toolset_20260401"}],"mcp_servers":[{"name":"peer"}],"skills":[{"skill_id":"skill"}],"metadata":{"origin":"parent"},"multiagent":{"subagents":{"type":"enabled"}}}`)
	for _, enabled := range []bool{false, true} {
		raw, err := trustedDSHChildSnapshot(json.RawMessage(`{"type":"inline","name":"child","system":"child system"}`), parent, &RuntimeSubagentConfig{InlineAgents: enabled})
		if !enabled {
			if err == nil {
				t.Fatal("disabled inline accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var got, source map[string]json.RawMessage
		if err = json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(parent, &source); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"id", "version", "multiagent", "description"} {
			if _, present := got[key]; present {
				t.Fatal("inline inherited saved identity or delegation", key)
			}
		}
		for _, key := range []string{"model", "tools", "mcp_servers", "skills", "metadata"} {
			if string(got[key]) != string(source[key]) {
				t.Fatal("inline inheritance changed", key)
			}
		}
		if string(got["type"]) != `"inline"` || string(got["name"]) != `"child"` || string(got["system"]) != `"child system"` {
			t.Fatal("inline identity lost")
		}
	}
}

func TestDSHChildPublicationUsesPersistedRegistrationAfterHotUpdate(t *testing.T) {
	old := json.RawMessage(`{"type":"agent","id":"parent","version":1,"name":"self","tools":[{"name":"Bash"}]}`)
	updated := json.RawMessage(`{"type":"agent","id":"parent","version":1,"name":"self","tools":[{"name":"Read"}]}`)
	frozenConfig := RuntimeSubagentConfig{Agents: []RuntimeSubagentDefinition{{Name: "self", AgentSnapshot: old}}}
	configJSON, err := json.Marshal(managedAgentRuntimeConfigSchema{Subagents: &frozenConfig})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := json.Marshal(dshContextSnapshot{Config: configJSON})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := registeredDSHSubagents(persisted)
	if err != nil {
		t.Fatal(err)
	}
	live := RuntimeSubagentConfig{Agents: []RuntimeSubagentDefinition{{Name: "self", AgentSnapshot: updated}}}
	if _, err := trustedDSHChildSnapshot(old, updated, &live); err == nil {
		t.Fatal("live configuration unexpectedly accepts old snapshot")
	}
	got, err := trustedDSHChildSnapshot(old, updated, frozen)
	if err != nil || string(got) != string(old) {
		t.Fatal("registered child rejected after hot update", err)
	}
	if _, err := trustedDSHChildSnapshot(updated, updated, frozen); err == nil {
		t.Fatal("unregistered mutated child accepted")
	}
	for _, bad := range [][]byte{nil, []byte(`{`), []byte(`{"config":{}}`)} {
		if _, err := registeredDSHSubagents(bad); err == nil {
			t.Fatal("missing registered authority accepted")
		}
	}
}

func TestDSHRegisteredSelfReplaysRecordedOVR010HotUpdate(t *testing.T) {
	raw, err := os.ReadFile("testdata/ovr010-hot-self-907e.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Registered json.RawMessage `json:"registered_self"`
		Live       json.RawMessage `json:"live_parent_after_update"`
	}
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	var identity struct {
		Name string `json:"name"`
	}
	if err = json.Unmarshal(source.Registered, &identity); err != nil {
		t.Fatal(err)
	}
	config := RuntimeSubagentConfig{Agents: []RuntimeSubagentDefinition{{Name: identity.Name, AgentSnapshot: source.Registered}}}
	encoded, err := json.Marshal(managedAgentRuntimeConfigSchema{Subagents: &config})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := json.Marshal(dshContextSnapshot{Config: encoded})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := registeredDSHSubagents(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = trustedDSHChildSnapshot(source.Registered, source.Live, registered); err != nil {
		t.Fatal("recorded registered Bash self rejected after live Read patch", err)
	}
	var changed map[string]json.RawMessage
	if err = json.Unmarshal(source.Registered, &changed); err != nil {
		t.Fatal(err)
	}
	changed["tools"] = json.RawMessage(`[{"name":"Read"}]`)
	mutated, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = trustedDSHChildSnapshot(mutated, source.Live, registered); err == nil {
		t.Fatal("unregistered tool mutation accepted")
	}
	changed["name"] = json.RawMessage(`"foreign"`)
	foreign, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = trustedDSHChildSnapshot(foreign, source.Live, registered); err == nil {
		t.Fatal("unregistered expert accepted")
	}
}

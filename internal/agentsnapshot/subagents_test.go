package agentsnapshot

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"slices"
	"testing"
)

func TestSubagentNamesAndPinnedLookup(t *testing.T) {
	if _, err := SubagentNames([]json.RawMessage{json.RawMessage(`{`)}); err == nil {
		t.Fatal("accepted malformed child snapshot")
	}
	snapshot := json.RawMessage(`{"multiagent":{"agents":[{"id":"agent_a","name":"计算Agent","version":1,"system":"v1"},{"id":"agent_b","name":"计算Agent","version":2,"system":"v2"},{"id":"agent_c","name":"计算Agent-2","version":3},{"id":"agent_d","name":"Explore","version":4},{"id":"agent_e","name":"-reviewer","version":5},{"id":"agent_f","version":6},{"id":"agent_g","name":"agent_a","version":7}]}}`)
	var roster struct {
		Multiagent struct {
			Agents []json.RawMessage `json:"agents"`
		} `json:"multiagent"`
	}
	if err := jsonv2.Unmarshal(snapshot, &roster); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"unknown", "agent_foreign", ""} {
		if _, err := ForSubagentType(snapshot, name); err == nil {
			t.Fatalf("accepted unknown subagent %q", name)
		}
	}
	names, err := SubagentNames(roster.Multiagent.Agents)
	want := []string{"计算Agent", "计算Agent-2", "计算Agent-2-2", "Explore-2", "subagent-reviewer", "subagent", "agent_a-2"}
	if err != nil || !slices.Equal(names, want) {
		t.Fatalf("names=%v, want=%v, error=%v", names, want, err)
	}
	for i, name := range names {
		child, err := ForSubagentType(snapshot, name)
		if err != nil || string(child) != string(roster.Multiagent.Agents[i]) {
			t.Fatalf("name=%q child=%s, want=%s, error=%v", name, child, roster.Multiagent.Agents[i], err)
		}
	}
	legacy, err := ForSubagentType(snapshot, "agent_b")
	if err != nil || string(legacy) != string(roster.Multiagent.Agents[1]) {
		t.Fatalf("legacy child=%s, error=%v", legacy, err)
	}
}

func TestForSubagentRejectsUnreferencedAgent(t *testing.T) {
	snapshot := json.RawMessage(`{"id":"agent_parent","multiagent":{"agents":[{"id":"agent_child","version":2,"system":"pinned"}]}}`)
	for _, id := range []string{"agent_parent", "agent_foreign", ""} {
		if _, err := ForSubagent(snapshot, id); err == nil {
			t.Fatalf("accepted unreferenced agent %q", id)
		}
	}
	child, err := ForSubagent(snapshot, "agent_child")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Version int    `json:"version"`
		System  string `json:"system"`
	}
	if err := jsonv2.Unmarshal(child, &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != 2 || config.System != "pinned" {
		t.Fatalf("did not retain pinned child: %+v", config)
	}
}

func TestResolveStoredSnapshot(t *testing.T) {
	for _, source := range []string{
		`{`,
		`{"multiagent":{"agents":[{"type":"agent","version":1}]}}`,
		`{"multiagent":{"agents":[{"id":"agent_child","version":0}]}}`,
		`{"multiagent":{"agents":[{"id":"agent_child","version":-1}]}}`,
	} {
		if _, err := resolveSnapshot(t.Context(), nil, "workspace", json.RawMessage(source)); err == nil {
			t.Fatalf("accepted invalid snapshot: %s", source)
		}
	}
	for _, source := range []string{
		`{"model":{"id":"parent"},"future":9007199254740993}`,
		`{"multiagent":{"type":"coordinator","agents":[{"id":"agent_child","version":1,"name":"计算Agent","model":{"id":"pinned"},"system":"frozen"}]},"future":9007199254740993}`,
	} {
		resolved, err := resolveSnapshot(t.Context(), nil, "workspace", json.RawMessage(source))
		if err != nil || string(resolved) != source {
			t.Fatalf("changed complete snapshot: %s %v", resolved, err)
		}
	}
	source := json.RawMessage(`{"id":"agent_parent","version":1,"model":{"id":"parent-v1"},"system":"session-specific","multiagent":{"type":"coordinator","agents":[{"id":"agent_frozen","version":2,"model":{"id":"child-v2"}},{"id":"agent_parent","version":1}]},"future":9007199254740993}`)
	resolved, err := resolveSnapshot(t.Context(), nil, "workspace", source)
	if err != nil {
		t.Fatal(err)
	}
	self, err := ForSubagent(resolved, "agent_parent")
	if err != nil {
		t.Fatal(err)
	}
	var child map[string]json.RawMessage
	if err := jsonv2.Unmarshal(self, &child); err != nil {
		t.Fatal(err)
	}
	if string(child["system"]) != `"session-specific"` || string(child["multiagent"]) != "null" || string(child["future"]) != "9007199254740993" {
		t.Fatalf("lost frozen self config: %s", self)
	}
	frozen, err := ForSubagent(resolved, "agent_frozen")
	if err != nil || string(frozen) != `{"id":"agent_frozen","version":2,"model":{"id":"child-v2"}}` {
		t.Fatalf("changed already expanded child: %s %v", frozen, err)
	}
}

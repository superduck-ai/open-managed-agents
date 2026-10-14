package agentsnapshot

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

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
	var roster agentRosterSnapshot
	if err := jsonv2.Unmarshal(resolved, &roster); err != nil {
		t.Fatal(err)
	}
	self := roster.Multiagent.Agents[1]
	var child map[string]json.RawMessage
	if err := jsonv2.Unmarshal(self, &child); err != nil {
		t.Fatal(err)
	}
	if string(child["system"]) != `"session-specific"` || string(child["multiagent"]) != "null" || string(child["future"]) != "9007199254740993" {
		t.Fatalf("lost frozen self config: %s", self)
	}
	frozen := roster.Multiagent.Agents[0]
	if string(frozen) != `{"id":"agent_frozen","version":2,"model":{"id":"child-v2"}}` {
		t.Fatalf("changed already expanded child: %s %v", frozen, err)
	}
}

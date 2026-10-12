package agentsnapshot

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

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

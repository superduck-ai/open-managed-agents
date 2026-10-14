package agentsnapshot

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestResolveRejectsInvalidReferences(t *testing.T) {
	for _, source := range []string{
		`{`,
		`{"agents":[{"version":1}]}`,
		`{"agents":[{"id":"agent_child","version":0,"model":{"id":"model"}}]}`,
		`{"agents":[{"id":"agent_child","version":-1}]}`,
	} {
		if _, err := Resolve(t.Context(), nil, db.Agent{Multiagent: json.RawMessage(source)}); err == nil {
			t.Fatalf("accepted invalid references: %s", source)
		}
	}
}

func TestResolveCurrentSelfUsesAgentCopy(t *testing.T) {
	agent := db.Agent{ExternalID: "agent_parent", CurrentVersion: 1, Name: "parent", System: new("parent-system"), Model: json.RawMessage(`{"id":"parent-model"}`), Multiagent: json.RawMessage(`{"type":"coordinator","agents":[{"id":"agent_parent","version":1}]}`)}
	resolved, err := Resolve(t.Context(), nil, agent)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Multiagent subagentRoster `json:"multiagent"`
	}
	if err := jsonv2.Unmarshal(resolved, &root); err != nil {
		t.Fatal(err)
	}
	child := agent
	child.Multiagent = nil
	want, err := FromAgent(child)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Multiagent.Agents) != 1 || !SameRawJSON(root.Multiagent.Agents[0], want) || string(agent.Multiagent) != `{"type":"coordinator","agents":[{"id":"agent_parent","version":1}]}` {
		t.Fatalf("self changed parent or lost configuration: %s", resolved)
	}
}

package codesessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestManagedAgentRuntimeConfigRejectsInvalidKnownFields(t *testing.T) {
	service := &Service{}
	for _, source := range []string{
		`{"system_prompt":42}`,
		`{"runtime_skills":{}}`,
		`{"subagents":{"agents":"invalid"}}`,
	} {
		if _, err := service.managedAgentRuntimeConfig(t.Context(), db.Session{}, json.RawMessage(source)); err == nil {
			t.Fatalf("accepted invalid runtime config: %s", source)
		}
	}
}

func TestManagedAgentRuntimeConfigPreservesExtensionsAndPinsSubagents(t *testing.T) {
	service := &Service{}
	session := db.Session{AgentSnapshot: json.RawMessage(`{"id":"parent","version":1,"name":"reader","system":"pinned","multiagent":{"type":"coordinator","agents":[{"type":"agent","id":"parent","version":1}]},"tools":[]}`)}
	source := json.RawMessage(`{"system_prompt":"instructions","runtime_skills":[],"extension":{"precise":9007199254740993},"subagents":{"agents":[]}}`)
	result, err := service.managedAgentRuntimeConfig(t.Context(), session, source)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(result, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["extension"]) != `{"precise":9007199254740993}` || string(fields["system_prompt"]) != `"instructions"` || string(fields["runtime_skills"]) != `[]` {
		t.Fatalf("runtime config fields changed: %s", result)
	}
	var subagents RuntimeSubagentConfig
	if err := json.Unmarshal(fields["subagents"], &subagents); err != nil {
		t.Fatal(err)
	}
	if len(subagents.Agents) != 1 || subagents.Agents[0].Name != "reader" || subagents.Agents[0].SystemPrompt != "pinned" {
		t.Fatalf("subagents = %+v", subagents)
	}
}

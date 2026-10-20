package codesessions

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestRuntimeSubagentsRejectInvalidRoster(t *testing.T) {
	for _, snapshot := range []string{
		`invalid`,
		`{"multiagent":{"type":"other","agents":[]}}`,
		`{"multiagent":{"type":"coordinator","agents":[]}}`,
		`{"multiagent":{"type":"coordinator","agents":[{"type":"agent","id":"child"}]}}`,
		`{"multiagent":{"type":"coordinator","agents":[{"type":"self"}]}}`,
	} {
		t.Run(snapshot, func(t *testing.T) {
			if _, err := resolveRuntimeSubagents(t.Context(), nil, db.Session{AgentSnapshot: json.RawMessage(snapshot)}); err == nil {
				t.Fatal("invalid roster silently dropped")
			}
		})
	}
}

func TestRuntimeSubagentsSelfAndDuplicateNames(t *testing.T) {
	session := db.Session{AgentSnapshot: json.RawMessage(`{"id":"parent","version":4,"name":"self_probe","system":"session override","multiagent":{"type":"coordinator","agents":[{"type":"agent","id":"parent","version":4}]},"tools":[{"type":"agent_toolset_20260401"}]}`)}
	config, err := resolveRuntimeSubagents(t.Context(), nil, session)
	if err != nil || len(config.Agents) != 1 || config.Agents[0].SystemPrompt != "session override" || len(config.Agents[0].Tools) != 8 {
		t.Fatalf("self reference not resolved from pinned snapshot: %+v %v", config, err)
	}
	session.AgentSnapshot = json.RawMessage(`{"id":"parent","version":4,"name":"self_probe","multiagent":{"type":"coordinator","agents":[{"type":"agent","id":"parent","version":4},{"type":"agent","id":"parent","version":4}]}}`)
	if _, err := resolveRuntimeSubagents(t.Context(), nil, session); !errors.Is(err, errRuntimeSubagentConfig) {
		t.Fatalf("ambiguous names accepted: %v", err)
	}
}

func TestRuntimeSubagentsDefinitionAndEmptyTools(t *testing.T) {
	if config, err := resolveRuntimeSubagents(t.Context(), nil, db.Session{AgentSnapshot: json.RawMessage(`{"system":"ordinary agent"}`)}); err != nil || config != nil {
		t.Fatalf("ordinary agent changed: %+v %v", config, err)
	}
	for _, tools := range []string{`[]`, `[{"type":"agent_toolset_20260401","default_config":{"enabled":false}}]`, `[{"type":"mcp_toolset"}]`} {
		var snapshot runtimeAgentSnapshot
		if err := json.Unmarshal([]byte(`{"name":"reader","tools":`+tools+`}`), &snapshot); err != nil {
			t.Fatal(err)
		}
		definition, err := runtimeSubagentDefinition(snapshot)
		if err != nil || definition.Tools == nil || len(definition.Tools) != 0 {
			t.Fatalf("disabled tools widened to inherited tools: %+v %v", definition, err)
		}
	}
	for _, model := range []string{`"child-model"`, `{"id":"child-model"}`} {
		var snapshot runtimeAgentSnapshot
		if err := json.Unmarshal([]byte(`{"name":"reader","description":"child","system":"pinned instructions","model":`+model+`,"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"bash","enabled":true,"permission_policy":{"type":"always_ask"}}]}]}`), &snapshot); err != nil {
			t.Fatal(err)
		}
		definition, err := runtimeSubagentDefinition(snapshot)
		expected := RuntimeSubagentDefinition{Name: "reader", Description: "child", SystemPrompt: "pinned instructions", Model: "child-model", Tools: []string{"Bash"}}
		if err != nil || !reflect.DeepEqual(definition, expected) {
			t.Fatalf("definition changed: %+v %v", definition, err)
		}
	}
}

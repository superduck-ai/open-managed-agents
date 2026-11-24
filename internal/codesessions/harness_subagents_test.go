package codesessions

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxtools"
)

func TestHarnessSubagentToolPolicies(t *testing.T) {
	session := db.Session{AgentSnapshot: json.RawMessage(`{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]}`)}
	claim := db.HarnessTurnClaim{}
	tools, err := HarnessExecutionTools(session, claim)
	if err != nil || slices.ContainsFunc(tools, func(tool sandboxtools.Definition) bool { return isHarnessDelegationTool(tool.Name) }) {
		t.Fatalf("delegation was offered on a session without a roster: %+v, %v", tools, err)
	}
	claim.CodeSession.Metadata = json.RawMessage(`{"config":{"subagents":{"inlineAgents":false,"agents":[{"name":"Research","description":"Inspect files"}]}}}`)
	tools, err = HarnessExecutionTools(session, claim)
	if err != nil || !slices.ContainsFunc(tools, func(tool sandboxtools.Definition) bool {
		return tool.Name == "subagent" && json.Valid(tool.InputSchema)
	}) {
		t.Fatalf("delegation tool is missing or invalid: %+v, %v", tools, err)
	}
	claim.Subagent = &db.SessionThread{AgentSnapshot: json.RawMessage(`{"name":"Research","model":{"id":"specialist"},"system":"Examine files","tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true},{"name":"bash","enabled":true}]}]}`)}
	tools, err = HarnessExecutionTools(session, claim)
	if err != nil || len(tools) != 1 || tools[0].Name != "Read" {
		t.Fatalf("child tools exceeded the intersection of parent and child tools: %+v, %v", tools, err)
	}
	model, prompt, err := HarnessSubagentModel(*claim.Subagent, "parent-model")
	if err != nil || model != "specialist" || prompt == "" {
		t.Fatalf("child used incorrect model configuration: %q, %q, %v", model, prompt, err)
	}
}

func TestHarnessSubagentPresetValidation(t *testing.T) {
	handler := &Handler{}
	config := &RuntimeSubagentConfig{Agents: []RuntimeSubagentDefinition{
		{Name: "Research", AgentSnapshot: json.RawMessage(`{"name":"Research"}`)},
	}}
	args := harnessDelegationArgs{Description: "Inspect", Prompt: "Read the project", Preset: "Unknown"}
	_, failure, err := handler.buildHarnessDelegation(t.Context(), db.CodeSession{}, config, "subagent", args)
	if err != nil || failure == "" {
		t.Fatalf("unknown specialist was accepted: %q, %v", failure, err)
	}
	args.Preset, args.FreshContext = "Research", new(false)
	delegation, failure, err := handler.buildHarnessDelegation(t.Context(), db.CodeSession{}, config, "subagent", args)
	if err != nil || failure != "" || !delegation.InheritContext || string(delegation.AgentSnapshot) != `{"name":"Research"}` {
		t.Fatalf("could not resolve configured specialist: %+v, %q, %v", delegation, failure, err)
	}
}

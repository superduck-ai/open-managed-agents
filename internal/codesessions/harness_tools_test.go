package codesessions

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxtools"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestHarnessSandboxToolsFollowBrainCatalog(t *testing.T) {
	tools, err := HarnessSandboxTools(db.Session{AgentSnapshot: json.RawMessage(`{"tools":[]}`)})
	if err != nil || len(tools) != 0 {
		t.Fatalf("unconfigured sandbox tools were offered: %+v, %v", tools, err)
	}

	tools, err = HarnessSandboxTools(db.Session{AgentSnapshot: json.RawMessage(`{"tools":[{
		"type":"agent_toolset_20260401",
		"default_config":{"enabled":false},
		"configs":[
			{"name":"bash","enabled":true,"permission_policy":{"type":"always_ask"}},
			{"name":"read","enabled":true,"permission_policy":{"type":"always_allow"}}
		]
	}]}`)})
	if err != nil || len(tools) != 1 || tools[0].Name != "Read" || tools[0].RemoteName != "read" || !json.Valid(tools[0].InputSchema) {
		t.Fatalf("sandbox tools did not follow the configured policies: %+v, %v", tools, err)
	}
}

func TestHarnessSandboxOutcome(t *testing.T) {
	lost := harnessSandboxResult(db.SandboxToolCall{State: "lost"})
	if !lost.Failed || lost.Text == "" {
		t.Fatalf("lost call has no error: %+v", lost)
	}
	image := harnessSandboxResult(db.SandboxToolCall{State: "succeeded", Result: json.RawMessage(`{"content":[{"type":"image"}]}`)})
	if !image.Failed {
		t.Fatalf("image result passed as text: %+v", image)
	}
	success := harnessSandboxResult(db.SandboxToolCall{State: "succeeded", Result: json.RawMessage(`{"content":[{"type":"text","text":"hello"}]}`)})
	if success.Failed || success.Text != "hello" {
		t.Fatalf("unexpected tool result: %+v", success)
	}
}

func TestHarnessChoiceToolUsesExistingQuestionEvent(t *testing.T) {
	isQuestion := func(tool sandboxtools.Definition) bool { return tool.Name == sessioncontract.HarnessAskUserQuestion }
	tools, err := HarnessModelTools(db.Session{})
	if err != nil || slices.ContainsFunc(tools, isQuestion) {
		t.Fatalf("question tool was offered without explicit permission: %v", err)
	}
	snapshot := json.RawMessage(`{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"ask_user_question","enabled":true,"permission_policy":{"type":"always_ask"}}]}]}`)
	tools, err = HarnessModelTools(db.Session{AgentSnapshot: snapshot})
	if err != nil || !slices.ContainsFunc(tools, isQuestion) {
		t.Fatalf("enabled question tool is missing: %v", err)
	}
	for _, tool := range tools {
		if !json.Valid(tool.InputSchema) {
			t.Fatalf("invalid schema for %s", tool.Name)
		}
	}
	tool := ModelRequestToolUse{ID: "pick", Name: sessioncontract.HarnessAskUserQuestion,
		Input: json.RawMessage(`{"questions":[{"id":"color","question":"Pick a color","options":[{"label":"Blue"},{"label":"Green"}]}]}`)}
	if !validHarnessTool(ModelRequestResult{ToolUses: []ModelRequestToolUse{tool}, ToolUseIDs: []string{tool.ID}}) {
		t.Fatal("valid question was rejected")
	}
	request := &ModelRequest{CodeSessionID: "cse_test", harnessLease: &db.HarnessTurnLease{}}
	identity := toolIdentity{Kind: "agent_toolset", ToolName: "ask_user_question"}
	raw, err := modelToolUsePayload(request, tool, identity, resolvedToolPermissionAsk, false, time.Now().UTC())
	var event struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err != nil || json.Unmarshal(raw, &event) != nil || event.Type != "agent.custom_tool_use" || event.Name != sessioncontract.HarnessAskUserQuestion {
		t.Fatalf("question event was not published: %s, %v", raw, err)
	}
}

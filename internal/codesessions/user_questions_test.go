package codesessions

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func TestUserQuestionModelPreviewWaitsForWorkerRegistration(t *testing.T) {
	for _, name := range []string{"AskUserQuestion", "ask_user_question"} {
		tool := ModelRequestToolUse{ID: "call", Name: name, Input: json.RawMessage(`{"questions":[]}`)}
		for _, external := range []bool{false, true} {
			event, err := modelToolUsePayload(&ModelRequest{CodeSessionID: "code"}, tool, parseClaudeToolIdentity(name), resolvedToolPermissionAllow, external, time.Now())
			if err != nil || len(event) != 0 {
				t.Fatalf("question preview exposed before registration: %s %v", event, err)
			}
		}
	}
}

func TestUserQuestionPublicIdentityPreservesMCPTools(t *testing.T) {
	for _, name := range []string{"AskUserQuestion", "ask_user_question", "mcp__server__ask_user_question"} {
		kind, publicName := toolPermissionPublicIdentity(name, parseClaudeToolIdentity(name))
		wantKind, wantName := "agent.custom_tool_use", "AskUserQuestion"
		if name == "mcp__server__ask_user_question" {
			wantKind, wantName = "agent.mcp_tool_use", "ask_user_question"
		}
		if kind != wantKind || publicName != wantName {
			t.Fatalf("%s identity = %s %s", name, kind, publicName)
		}
	}
}

func TestUserQuestionRuntimeRequiresExplicitEnablement(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		snapshot := `{"tools":[{"type":"agent_toolset_20260401"}]}`
		if enabled {
			snapshot = `{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"ask_user_question","enabled":true,"permission_policy":{"type":"always_allow"}}]}]}`
		}
		tools, err := runtimeRootToolsets(json.RawMessage(snapshot))
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(enabledSandboxTools(tools), "ask_user_question") != enabled {
			t.Fatalf("question enablement = %v", enabledSandboxTools(tools))
		}
	}
}

package codesessions

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDelegationPermissionRequiresEnabledConfiguration(t *testing.T) {
	for _, tc := range []struct {
		multiagent string
		allowed    bool
	}{
		{`null`, false},
		{`{"type":"coordinator","agents":[]}`, false},
		{`{"type":"multiagent_20261001","subagents":{"type":"disabled"}}`, false},
		{`{"type":"multiagent_20261001","subagents":{"type":"unknown"}}`, false},
		{`{"type":"coordinator","agents":[{"id":"worker"}]}`, true},
		{`{"type":"multiagent_20261001"}`, true},
		{`{"type":"multiagent_20261001","subagents":{"type":"enabled"}}`, true},
	} {
		for _, name := range []string{"list_agents", "send_to_agent", "archive_agent"} {
			permission, identity := resolveToolPermissionFromAgentSnapshot(json.RawMessage(`{"multiagent":`+tc.multiagent+`}`), name)
			if (permission == resolvedToolPermissionAllow) != tc.allowed || identity.Kind != "delegation" {
				t.Fatalf("%s %s: %s %+v", tc.multiagent, name, permission, identity)
			}
		}
	}
}

func TestUnknownToolCallIsDeniedWithoutInventedEvaluation(t *testing.T) {
	permission, identity := resolveToolPermissionFromAgentSnapshot(json.RawMessage(`{"tools":[]}`), "unknown_tool")
	_, raw, err := toolCallPublicPayload("cse_test", toolPermissionRequest{ToolName: "unknown_tool", ToolUseID: "call"}, identity, permission, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil || payload["evaluated_permission"] != "deny" || payload["evaluation"] != nil {
		t.Fatalf("%s", raw)
	}
}

package codesessions

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestFreshResolvedAskPublicPayloadRetainsPolicyAndActualDSRIdentity(t *testing.T) {
	snapshot := json.RawMessage(`{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false,"permission_policy":{"type":"always_allow"}},"configs":[{"name":"read","enabled":true,"permission_policy":{"type":"always_ask"}}]}]}`)
	permission, identity := resolveToolPermissionFromAgentSnapshot(snapshot, "Read")
	if permission != resolvedToolPermissionAsk || identity.Automatic {
		t.Fatal("not explicit resolved ask")
	}
	at := time.Date(2026, 11, 11, 14, 50, 9, 0, time.UTC)
	input := json.RawMessage(`{"file_path":"/root/.claude/skills/owned/SKILL.md","precise":9007199254740993}`)
	for _, thread := range []string{"", "child-owned"} {
		request := toolPermissionRequest{RequestID: "request-owned", ToolUseID: "call-owned", ToolName: "Read", SessionThreadID: thread}
		first, raw, err := dshToolCallPublicPayload("code-owned", request, input, identity, permission, at, false)
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			ID         string          `json:"id"`
			Type       string          `json:"type"`
			Name       string          `json:"name"`
			Input      json.RawMessage `json:"input"`
			Permission string          `json:"evaluated_permission"`
			Evaluation struct {
				Type string `json:"type"`
			} `json:"evaluation"`
			Thread string `json:"session_thread_id"`
		}
		if err = json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.Permission != "ask" || event.Evaluation.Type != "always_ask" || event.Type != "agent.tool_use" || event.Name != "read" || event.Thread != thread || !bytes.Equal(event.Input, input) {
			t.Fatal("fresh public contract changed or absent", string(raw))
		}
		again, replayed, err := dshToolCallPublicPayload("code-owned", request, input, identity, permission, at, false)
		if err != nil || again.PublicEventID != first.PublicEventID || !bytes.Equal(replayed, raw) {
			t.Fatal("unstable identity/payload replay")
		}
	}
}

func TestAskEvaluationDoesNotInventPolicyForDeniedOrCustomCalls(t *testing.T) {
	for _, tc := range []struct {
		name       string
		identity   toolIdentity
		permission resolvedToolPermission
		want       string
	}{
		{"disabled", toolIdentity{Kind: "agent_toolset", ToolName: "read"}, resolvedToolPermissionDeny, ""},
		{"automatic", toolIdentity{Kind: "agent_toolset", ToolName: "read", Automatic: true}, resolvedToolPermissionAsk, "auto"},
		{"custom", toolIdentity{Kind: "agent_toolset", ToolName: "ask_user_question"}, resolvedToolPermissionAsk, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, raw, err := dshToolCallPublicPayload("code-owned", toolPermissionRequest{ToolName: tc.identity.ToolName, ToolUseID: "call-owned"}, json.RawMessage(`{}`), tc.identity, tc.permission, time.Now().UTC(), false)
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Evaluation struct {
					Type string `json:"type"`
				} `json:"evaluation"`
			}
			if err = json.Unmarshal(raw, &event); err != nil || event.Evaluation.Type != tc.want {
				t.Fatal("unrelated evaluation changed", string(raw), err)
			}
		})
	}
}

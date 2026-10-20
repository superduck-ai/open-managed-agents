package codesessions

import (
	"encoding/json"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"reflect"
	"testing"
)

func TestSelfHostedToolDecisionRejectsUnlinkedResults(t *testing.T) {
	request := toolPermissionRequest{PublicEventID: "public", EventType: "agent.tool_use", RequestID: "request", ToolUseID: "provider", SessionThreadID: "thread", Input: map[string]any{"content": "original"}}
	valid := selfHostedToolResult{ToolUseID: "public", SessionThreadID: "thread", Content: []selfHostedToolContent{{Type: "text", Text: "exact\n bytes "}}}
	for _, name := range []string{"wrong id", "wrong thread", "nontext", "empty content", "custom call", "missing request", "missing provider"} {
		t.Run(name, func(t *testing.T) {
			r := request
			v := valid
			v.Content = append([]selfHostedToolContent(nil), valid.Content...)
			switch name {
			case "wrong id":
				v.ToolUseID = "foreign"
			case "wrong thread":
				v.SessionThreadID = "foreign"
			case "nontext":
				v.Content[0].Type = "image"
			case "empty content":
				v.Content = nil
			case "custom call":
				r.EventType = "agent.custom_tool_use"
			case "missing request":
				r.RequestID = ""
			case "missing provider":
				r.ToolUseID = ""
			}
			if _, err := selfHostedToolDecision(r, v); err == nil {
				t.Fatal("unlinked result accepted")
			}
		})
	}
	for _, failed := range []bool{true, false} {
		result := valid
		result.IsError = failed
		response, err := selfHostedToolDecision(request, result)
		if err != nil {
			t.Fatal(err)
		}
		if response["behavior"] != "allow" || !reflect.DeepEqual(response["updatedInput"], request.Input) {
			t.Fatal("decision/input changed")
		}
		external := response["externalResult"].(map[string]any)
		if external["isError"] != failed || !reflect.DeepEqual(external["content"], valid.Content) {
			t.Fatal("result bytes/error changed")
		}
	}
}

func TestSelfHostedPendingResultUsesOnlyExistingScopedPublicIDResolution(t *testing.T) {
	const code = "cse_test"
	base := maevents.ToolUseEventID(code, "provider", "child")
	canonical := derivedPrimarySessionEventID(code, base, "agent.tool_use")
	request := toolPermissionRequest{PublicEventID: canonical, EventType: "agent.tool_use", RequestID: "request", ToolUseID: "provider", SessionThreadID: "child"}
	metadata, err := json.Marshal(map[string]any{toolPermissionRequestMetadataKey(canonical): request})
	if err != nil {
		t.Fatal(err)
	}
	original := selfHostedToolResult{ToolUseID: base, Content: []selfHostedToolContent{{Type: "text", Text: "actual"}}}
	for _, id := range []string{"foreign", maevents.ToolUseEventID("other-session", "provider", "child")} {
		invalid := original
		invalid.ToolUseID = id
		if _, _, err := selfHostedPendingResult(metadata, code, invalid); err == nil {
			t.Fatal("foreign scope accepted")
		}
	}
	for _, id := range []string{base, canonical} {
		result := original
		result.ToolUseID = id
		got, normalized, err := selfHostedPendingResult(metadata, code, result)
		if err != nil || got.PublicEventID != canonical || normalized.ToolUseID != canonical || result.ToolUseID != id {
			t.Fatal("valid scoped alias or original evidence changed", err)
		}
	}
}

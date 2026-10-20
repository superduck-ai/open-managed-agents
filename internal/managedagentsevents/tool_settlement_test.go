package managedagentsevents

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCompletedToolSettlesOnlyMatchingPermission(t *testing.T) {
	const request = `{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider","session_thread_id":"primary"}`
	const sibling = `{"public_event_id":"other","request_id":"other-request","provider_tool_use_id":"other-provider","session_thread_id":"child"}`
	raw := []byte(`{"managed_agent_tool_permission_request:tool":` + request + `,"managed_agent_tool_permission_request":` + request + `,"managed_agent_tool_permission_request:other":` + sibling + `,"task_summary":"keep"}`)
	for _, tc := range []struct {
		name, thread, reference string
		want                    []string
	}{
		{"unknown result", "primary", "unknown", []string{"other", "tool"}},
		{"foreign thread", "child", "tool", []string{"other", "tool"}},
		{"canonical result", "primary", "tool", []string{"other"}},
		{"provider-derived alias", "primary", ToolUseEventID("code", "provider", "primary"), []string{"other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settled, err := SettlePendingToolRequest(raw, "code", "primary", tc.thread, tc.reference)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := PendingToolEventIDs(settled, "primary", "")
			if err != nil || !reflect.DeepEqual(pending, tc.want) {
				t.Fatalf("pending=%v want=%v err=%v", pending, tc.want, err)
			}
			var metadata map[string]any
			if json.Unmarshal(settled, &metadata) != nil || metadata["task_summary"] != "keep" {
				t.Fatal("unrelated metadata changed")
			}
			again, err := SettlePendingToolRequest(settled, "code", "primary", tc.thread, tc.reference)
			if err != nil || string(again) != string(settled) {
				t.Fatal("settlement not idempotent")
			}
		})
	}
}
func TestCompletedToolDoesNotResurrectClearedOrMalformedPermission(t *testing.T) {
	raw := []byte(`{"managed_agent_tool_permission_request":{"public_event_id":"tool","request_id":"r","provider_tool_use_id":"provider"},"managed_agent_tool_permission_request:tool":null}`)
	settled, err := SettlePendingToolRequest(raw, "code", "primary", "primary", "tool")
	if err != nil || string(settled) != string(raw) {
		t.Fatal("cleared request changed")
	}
	if _, err := SettlePendingToolRequest([]byte(`{"managed_agent_tool_permission_request:tool":true}`), "code", "primary", "primary", "tool"); err == nil {
		t.Fatal("malformed request accepted")
	}
}

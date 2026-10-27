package db

import (
	"errors"
	"testing"
)

func TestPendingToolSystemContextAdmission(t *testing.T) {
	primary := SessionThread{ExternalID: "primary", Status: "idle"}
	pending := []byte(`{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider","session_thread_id":"primary"}}`)
	toolID := "tool"
	system := SessionEvent{EventType: "system.message"}
	result := SessionEvent{EventType: "user.custom_tool_result", ToolUseID: &toolID}
	for _, tc := range []struct {
		name, status string
		metadata     []byte
		events       []SessionEvent
		paused       bool
	}{
		{"ordinary idle", "idle", nil, []SessionEvent{system}, false},
		{"paused worker", "requires_action", pending, []SessionEvent{system}, true},
		{"pending status lag", "idle", pending, []SessionEvent{system}, true},
		{"worker pause without registry", "requires_action", nil, []SessionEvent{system}, true},
		{"system before result", "requires_action", pending, []SessionEvent{system, result}, true},
		{"valid result then system", "requires_action", pending, []SessionEvent{result, system}, false},
		{"confirmation is not a tool result", "requires_action", pending, []SessionEvent{{EventType: "user.tool_confirmation", ToolUseID: &toolID}, system}, true},
		{"user does not unblock system", "requires_action", pending, []SessionEvent{{EventType: "user.message"}, system}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := codeSessionInputStateRow{ExternalID: "code", Status: "active", WorkerStatus: tc.status, WorkerExternalMetadata: tc.metadata}
			_, err := validateSessionInputBatch(primary, worker, tc.events)
			if tc.paused && !errors.Is(err, ErrSessionInputPaused) {
				t.Fatalf("got %v, want pending action rejection", err)
			}
			if !tc.paused && err != nil {
				t.Fatalf("legal system context rejected: %v", err)
			}
		})
	}
}

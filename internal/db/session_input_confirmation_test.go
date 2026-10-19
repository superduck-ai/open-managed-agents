package db

import (
	"errors"
	"testing"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestSessionConfirmationInfersPendingOwnerWithoutLosingExplicitScope(t *testing.T) {
	child, primaryID := "child", "primary"
	for _, tc := range []struct {
		name, eventID, status string
		thread                *string
		cleared, wantOK       bool
	}{
		{"wrong explicit thread", "child-public", "active", &primaryID, false, false},
		{"unknown public ID", "unknown", "active", nil, false, false},
		{"inactive worker", "child-public", "terminated", nil, false, false},
		{"completed request", "child-public", "active", nil, true, false},
		{"implicit child public ID", "child-public", "active", nil, false, true},
		{"implicit child history ID", maevents.ToolUseEventID("code", "provider", child), "active", nil, false, true},
		{"explicit child", "child-public", "active", &child, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := []byte(`{"managed_agent_tool_permission_request:child-public":{"public_event_id":"child-public","request_id":"request","provider_tool_use_id":"provider","session_thread_id":"child"}}`)
			if tc.cleared {
				metadata = []byte(`{}`)
			}
			worker := codeSessionInputStateRow{ExternalID: "code", Status: tc.status, WorkerStatus: "requires_action", WorkerExternalMetadata: metadata}
			event := SessionEvent{EventType: "user.tool_confirmation", ToolUseID: &tc.eventID, ThreadExternalID: tc.thread}
			_, err := validateSessionInputBatch(SessionThread{ExternalID: primaryID, Status: "idle"}, worker, []SessionEvent{event})
			if tc.wantOK && err != nil {
				t.Fatal(err)
			}
			expected := ErrSessionInputConflict
			if tc.name == "unknown public ID" || tc.name == "completed request" {
				expected = ErrSessionToolConfirmationInvalid
			}
			if !tc.wantOK && !errors.Is(err, expected) {
				t.Fatalf("invalid confirmation accepted: %v", err)
			}
		})
	}
}

func TestNonAskConfirmationIsInvalidInput(t *testing.T) {
	toolID := "completed-read"
	_, err := validateSessionInputBatch(SessionThread{ExternalID: "primary", Status: "idle"}, codeSessionInputStateRow{ExternalID: "code", Status: "active", WorkerStatus: "idle", WorkerExternalMetadata: []byte(`{}`)}, []SessionEvent{{EventType: "user.tool_confirmation", ToolUseID: &toolID}})
	if !errors.Is(err, ErrSessionToolConfirmationInvalid) {
		t.Fatalf("got %v, want invalid tool confirmation", err)
	}
}

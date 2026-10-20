package db

import (
	"errors"
	"testing"
	"time"
)

func TestSettledChildInterruptTargetsAndLifecycle(t *testing.T) {
	primary := SessionThread{ExternalID: "primary", Status: "idle"}
	session := Session{WorkspaceUUID: "workspace", ExternalID: "session"}
	childID, parent := "child", "primary"
	for _, tc := range []struct {
		name, status, reason, workspace, session, target string
		pending, archived, noParent                      bool
		wantNoop, wantInvalid                            bool
	}{
		{name: "foreign workspace", status: "idle", reason: "end_turn", workspace: "other", wantInvalid: true},
		{name: "foreign session", status: "idle", reason: "end_turn", session: "other", wantInvalid: true},
		{name: "different target", status: "idle", reason: "end_turn", target: "other", wantInvalid: true},
		{name: "not child", status: "idle", reason: "end_turn", noParent: true, wantInvalid: true},
		{name: "terminated", status: "terminated", reason: "end_turn", wantInvalid: true},
		{name: "archived", status: "idle", reason: "end_turn", archived: true, wantInvalid: true},
		{name: "in flight", status: "running", reason: "end_turn"},
		{name: "tool pause", status: "idle", reason: "requires_action", pending: true},
		{name: "pending tool despite end turn", status: "idle", reason: "end_turn", pending: true},
		{name: "fault pause", status: "idle", reason: "retries_exhausted"},
		{name: "completed idle", status: "idle", reason: "end_turn", wantNoop: true},
		{name: "budget idle", status: "idle", reason: "budget_reached", wantNoop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := SessionThread{ExternalID: childID, ParentThreadExternalID: &parent, WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID, Status: tc.status}
			if tc.workspace != "" {
				target.WorkspaceUUID = tc.workspace
			}
			if tc.session != "" {
				target.SessionExternalID = tc.session
			}
			if tc.noParent {
				target.ParentThreadExternalID = nil
			}
			if tc.archived {
				now := time.Now()
				target.ArchivedAt = &now
			}
			worker := codeSessionInputStateRow{WorkerStatus: "idle"}
			if tc.pending {
				worker.WorkerExternalMetadata = []byte(`{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider","session_thread_id":"child"}}`)
			}
			address := childID
			if tc.target != "" {
				address = tc.target
			}
			event := SessionEvent{ExternalID: "interrupt", EventType: "user.interrupt", ThreadExternalID: &address}
			noop, err := settledChildInterrupt(session, primary, worker, event, target, &sessionStopReason{Type: tc.reason})
			if noop != tc.wantNoop || errors.Is(err, ErrSessionInterruptTargetInvalid) != tc.wantInvalid || err != nil && !tc.wantInvalid {
				t.Fatalf("noop=%t err=%v", noop, err)
			}
			if noop {
				changes := SessionEventChanges{Events: []SessionEvent{event, {ExternalID: "next-message", EventType: "user.message"}}, SettledInputEventIDs: []string{event.ExternalID}}
				queued := changes.WorkerInputEvents()
				if len(queued) != 1 || queued[0].ExternalID != "next-message" || len(changes.Events) != 2 {
					t.Fatal("settled interrupt dispatched or next message lost")
				}
			}
		})
	}
}

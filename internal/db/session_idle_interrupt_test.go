package db

import (
	"errors"
	"testing"
)

func TestIdleSessionInterruptAcceptance(t *testing.T) {
	child := "child"
	for _, tc := range []struct {
		name, status, workerStatus string
		thread                     *string
		wantOK                     bool
	}{
		{name: "terminated remains rejected", status: "terminated"},
		{name: "other idle thread not inferred", status: "idle", thread: &child},
		{name: "rescheduling worker not idle", status: "idle", workerStatus: "rescheduling"},
		{name: "fresh idle accepted", status: "idle", wantOK: true},
		{name: "idle worker accepted", status: "idle", workerStatus: "idle", wantOK: true},
		{name: "running unchanged", status: "running", workerStatus: "running", wantOK: true},
		{name: "tool pause unchanged", status: "idle", workerStatus: "requires_action", wantOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := SessionEvent{ExternalID: "interrupt", EventType: "user.interrupt", ThreadExternalID: tc.thread}
			id, err := validateSessionInputBatch(SessionThread{ExternalID: "primary", Status: tc.status}, codeSessionInputStateRow{WorkerStatus: tc.workerStatus}, []SessionEvent{event})
			if tc.wantOK && (err != nil || id != "") {
				t.Fatalf("interrupt admission=%q err=%v", id, err)
			}
			if !tc.wantOK && !errors.Is(err, ErrSessionInputConflict) {
				t.Fatalf("invalid interrupt admitted: %v", err)
			}
		})
	}
}

func TestIdleInterruptNeverDispatchesOrDropsFollowingMessage(t *testing.T) {
	events := []SessionEvent{{ExternalID: "idle-interrupt", EventType: "user.interrupt"}, {ExternalID: "message", EventType: "user.message"}, {ExternalID: "live-interrupt", EventType: "user.interrupt"}}
	changes := SessionEventChanges{Events: events, SettledInputEventIDs: []string{"idle-interrupt", "message"}}
	queued := changes.WorkerInputEvents()
	if len(queued) != 2 || queued[0].ExternalID != "message" || queued[1].ExternalID != "live-interrupt" || len(changes.Events) != 3 || changes.Events[0].ExternalID != "idle-interrupt" {
		t.Fatalf("dispatch altered public events or next task: %+v / %+v", queued, changes.Events)
	}
}

func TestIdleInterruptDoesNotBypassPendingToolWork(t *testing.T) {
	worker := codeSessionInputStateRow{WorkerStatus: "idle", WorkerExternalMetadata: []byte(`{"managed_agent_tool_permission_request:tool":{"public_event_id":"tool","request_id":"request","provider_tool_use_id":"provider","session_thread_id":"primary"}}`)}
	event := SessionEvent{ExternalID: "interrupt", EventType: "user.interrupt"}
	primary := SessionThread{ExternalID: "primary", Status: "idle"}
	if idleSessionInterrupt(primary, worker, event) {
		t.Fatal("pending tool work treated as empty idle")
	}
	if _, err := validateSessionInputBatch(primary, worker, []SessionEvent{event}); !errors.Is(err, ErrSessionInputConflict) {
		t.Fatalf("pending worker rule changed: %v", err)
	}
}

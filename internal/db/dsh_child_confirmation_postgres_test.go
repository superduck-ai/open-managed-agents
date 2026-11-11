//go:build integration

package db

import (
	"encoding/json"
	"testing"
	"time"
	"uuid"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestDSHChildConfirmationAliasesPostgres(t *testing.T) {
	for _, decision := range []string{"deny", "allow"} {
		for _, projected := range []bool{false, true} {
			name := "child-original"
			if projected {
				name = "primary-projection"
			}
			t.Run(name+"/"+decision, func(t *testing.T) {
				ctx, store, scope, parentPermission := newDSHConfirmationTestStore(t, "resolved")
				code, _, err := store.GetCodeSession(ctx, scope.CodeSessionID)
				if err != nil {
					t.Fatal(err)
				}
				primary, err := NewSessionThreadMapper(store.mapperDB).FindPrimary(ctx, scope.WorkspaceUUID, scope.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				child, err := store.CreateSessionThreadIfAbsent(ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "child-thread", OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: code.SessionUUID, SessionExternalID: scope.SessionID, ParentThreadUUID: &primary.UUID, ParentThreadExternalID: &primary.ExternalID, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = store.AppendDSHTranscript(ctx, scope, "child", nil, []DSHTranscriptRecord{{Offset: 0, Payload: []byte(`{"version":4,"id":"child"}`)}}); err != nil {
					t.Fatal(err)
				}
				if _, err = NewDSHExecutionMapper(store.mapperDB).BindTranscript(ctx, scope, "child", child.ExternalID); err != nil {
					t.Fatal(err)
				}
				permission := parentPermission
				permission.RequestID = "child-permission"
				permission.DSHSessionID = "child"
				permission.ThreadID = child.ExternalID
				permission.ToolUseID = "child-native"
				permission.PublicEventID = "child-projection"
				permission.State = "pending"
				permission.Decision = nil
				_, pendingChanges, err := store.RequestDSHPermission(ctx, scope, permission, nil)
				if err != nil {
					t.Fatal(err)
				}
				assertDSHChildStatusCopies(t, pendingChanges.Events, child.ExternalID, "session.thread_status_idle", "requires_action")
				id := maevents.ToolUseEventID(scope.CodeSessionID, permission.ToolUseID, child.ExternalID)
				if projected {
					id = permission.PublicEventID
				}
				payload, _ := json.Marshal(map[string]string{"type": "user.tool_confirmation", "tool_use_id": id, "result": decision})
				event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "child-confirmation", EventType: "user.tool_confirmation", ToolUseID: &id, Payload: payload, CreatedAt: time.Now().UTC()}
				changes, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event}, nil)
				if err != nil {
					t.Fatal(err)
				}
				assertDSHChildStatusCopies(t, changes.Events, child.ExternalID, "session.thread_status_running", "")
				var accepted SessionEvent
				for _, item := range changes.Events {
					if item.ExternalID == event.ExternalID {
						accepted = item
					}
				}
				if accepted.ToolUseID == nil || *accepted.ToolUseID != id {
					t.Fatal("accepted confirmation lost original reference")
				}
				pending, err := store.DSHPermissionConfirmationPending(ctx, scope, permission)
				if err != nil || !pending || !accepted.ProcessedAt.IsZero() {
					t.Fatalf("confirmation must remain queued: %v %v", pending, err)
				}
				processed, err := store.ProcessSessionInputsForWorker(ctx, code, scope.Epoch, []string{accepted.ExternalID})
				if err != nil || len(processed) != 1 || processed[0].ProcessedAt.IsZero() {
					t.Fatalf("processing receipt: %+v %v", processed, err)
				}
				pending, err = store.DSHPermissionConfirmationPending(ctx, scope, permission)
				if err != nil || pending {
					t.Fatalf("processed decision unavailable: %v %v", pending, err)
				}
				duplicate, err := store.ProcessSessionInputsForWorker(ctx, code, scope.Epoch, []string{accepted.ExternalID})
				if err != nil || len(duplicate) != 0 {
					t.Fatalf("duplicate processing: %+v %v", duplicate, err)
				}
				resolved, found, err := store.ResolvedDSHPermissionForInput(ctx, scope, accepted)
				if err != nil || !found || resolved.RequestID != permission.RequestID || resolved.State != "resolved" {
					t.Fatalf("resolved=%+v found=%v error=%v", resolved, found, err)
				}

				raw := []byte(`{"id":"child-idle","type":"session.thread_status_idle","session_thread_id":"child-thread","stop_reason":{"type":"end_turn"}}`)
				item := DSHPublicEvent{ID: "child-idle", Payload: raw, Events: []SessionEvent{{UUID: uuid.NewV4().String(), ExternalID: "child-idle", EventType: "session.thread_status_idle", ThreadExternalID: &child.ExternalID, StatusThreadID: child.ExternalID, Payload: raw, CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}}}
				done, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{item})
				if err != nil {
					t.Fatal(err)
				}
				assertDSHChildStatusCopies(t, done.Changes.Events, child.ExternalID, "session.thread_status_idle", "end_turn")
				replay, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{item})
				if err != nil || replay.Duplicates != 1 || len(replay.Changes.Events) != 0 {
					t.Fatalf("completed child revived or repeated: %+v %v", replay, err)
				}
			})
		}
	}
}

func assertDSHChildStatusCopies(t *testing.T, events []SessionEvent, child, kind, reason string) {
	t.Helper()
	owners := map[string]bool{}
	for _, event := range events {
		var payload sessionStatusPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.SessionThreadID != child {
			continue
		}
		var mirror struct {
			OriginalEventID string `json:"original_event_id"`
		}
		if err := json.Unmarshal(event.Payload, &mirror); err != nil {
			t.Fatal(err)
		}
		if mirror.OriginalEventID != "" && (payload.ProcessedAt.IsZero() || !payload.ProcessedAt.Equal(event.ProcessedAt)) {
			t.Fatal("status mirror payload disagrees with stored processing time")
		}
		if event.EventType != kind {
			t.Fatalf("unexpected child transition: %s", event.EventType)
		}
		actual := ""
		if payload.StopReason != nil {
			actual = payload.StopReason.Type
		}
		if reason != "" && actual != reason {
			t.Fatalf("reason %s, want %s", actual, reason)
		}
		if event.ThreadExternalID == nil {
			t.Fatal("missing history owner")
		}
		owners[*event.ThreadExternalID] = true
	}
	if len(owners) != 2 || !owners[child] {
		t.Fatalf("missing child/root status copies: %v", owners)
	}
}

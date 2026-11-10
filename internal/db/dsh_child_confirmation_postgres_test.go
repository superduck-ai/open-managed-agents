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
	for _, projected := range []bool{false, true} {
		name := "child-original"
		if projected {
			name = "primary-projection"
		}
		t.Run(name, func(t *testing.T) {
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
			if _, _, err = store.RequestDSHPermission(ctx, scope, permission, nil); err != nil {
				t.Fatal(err)
			}
			id := maevents.ToolUseEventID(scope.CodeSessionID, permission.ToolUseID, child.ExternalID)
			if projected {
				id = permission.PublicEventID
			}
			payload, _ := json.Marshal(map[string]string{"type": "user.tool_confirmation", "tool_use_id": id, "result": "allow"})
			event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "child-confirmation", EventType: "user.tool_confirmation", ToolUseID: &id, Payload: payload, CreatedAt: time.Now().UTC()}
			changes, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var accepted SessionEvent
			for _, item := range changes.Events {
				if item.ExternalID == event.ExternalID {
					accepted = item
				}
			}
			if accepted.ToolUseID == nil || *accepted.ToolUseID != id {
				t.Fatal("accepted confirmation lost original reference")
			}
			resolved, found, err := store.ResolvedDSHPermissionForInput(ctx, scope, accepted)
			if err != nil || !found || resolved.RequestID != permission.RequestID || resolved.State != "resolved" {
				t.Fatalf("resolved=%+v found=%v error=%v", resolved, found, err)
			}
		})
	}
}

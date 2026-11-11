//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"
)

func TestDSHArchivePreservesActivePublicationBoundaryPostgres(t *testing.T) {
	ctx, store, scope, permission := newDSHConfirmationTestStore(t, "resolved")
	code, _, err := store.GetCodeSession(ctx, scope.CodeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	primary, err := NewSessionThreadMapper(store.mapperDB).FindPrimary(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.CreateSessionThreadIfAbsent(ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "archive-pending-child", OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: code.SessionUUID, SessionExternalID: scope.SessionID, ParentThreadUUID: &primary.UUID, ParentThreadExternalID: &primary.ExternalID, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = store.ArchiveSessionThread(ctx, scope.WorkspaceUUID, scope.SessionID, child.ExternalID); !errors.Is(err, ErrInvalidState) {
		t.Fatal("running archive", err)
	}
	if _, err = store.AppendDSHTranscript(ctx, scope, "archive-child", nil, []DSHTranscriptRecord{{Offset: 0, Payload: []byte(`{"version":4,"id":"archive-child"}`)}}); err != nil {
		t.Fatal(err)
	}
	if _, err = NewDSHExecutionMapper(store.mapperDB).BindTranscript(ctx, scope, "archive-child", child.ExternalID); err != nil {
		t.Fatal(err)
	}
	permission.RequestID = "archive-pending-permission"
	permission.DSHSessionID = "archive-child"
	permission.ThreadID = child.ExternalID
	permission.ToolUseID = "archive-native"
	permission.PublicEventID = "archive-alias"
	permission.State = "pending"
	permission.Decision = nil
	if _, _, err = store.RequestDSHPermission(ctx, scope, permission, nil); err != nil {
		t.Fatal(err)
	}
	before, _, err := store.GetSession(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil || before.Status != "idle" {
		t.Fatal("requires action", before.Status, err)
	}
	end := coordinationPublicFixture("archive-model-end", "span.model_request_end", `{"id":"archive-model-end","type":"span.model_request_end","model_request_start_id":"archive-model-start","is_error":false,"event_ids":["archive-final"]}`)
	if _, err = store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{end}); err != nil {
		t.Fatal(err)
	}
	archived, removal, changed, err := store.ArchiveSessionThread(ctx, scope.WorkspaceUUID, scope.SessionID, child.ExternalID)
	if err != nil || !changed || archived.Status != "terminated" || archived.ArchivedAt == nil {
		t.Fatal("legal requires_action archive", archived, changed, err)
	}
	for _, event := range removal.StatusEvents {
		if event.EventType == "session.status_idle" {
			t.Fatal("archive crossed DSR finish/publication barrier", string(event.Payload))
		}
	}
	after, _, err := store.GetSession(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil || after.Status != "running" {
		t.Fatal("active input lost", after.Status, err)
	}
	if _, _, changed, err = store.ArchiveSessionThread(ctx, scope.WorkspaceUUID, scope.SessionID, child.ExternalID); err != nil || changed {
		t.Fatal("archive replay", changed, err)
	}
	pending, err := NewDSHExecutionMapper(store.mapperDB).Permissions(ctx, scope)
	if err != nil || len(dshPendingIDs(pending, "")) != 0 {
		t.Fatal("archived permission remains pending", err)
	}
	final := coordinationPublicFixture("archive-final", "agent.message", `{"id":"archive-final","type":"agent.message","content":[{"type":"text","text":"complete"}]}`)
	if _, err = store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{final}); err != nil {
		t.Fatal(err)
	}
	_, changes, err := store.FinishDSHInput(ctx, scope, "input", DSHFinish{Result: "success"})
	if err != nil {
		t.Fatal(err)
	}
	idle := false
	for _, event := range changes.Events {
		if event.EventType == "session.status_idle" {
			idle = true
			var p sessionStatusPayload
			if json.Unmarshal(event.Payload, &p) != nil || p.StopReason == nil || p.StopReason.Type != "end_turn" {
				t.Fatal("finish reason")
			}
		}
	}
	if !idle {
		t.Fatal("finish idle absent")
	}
}

//go:build integration

package db

import (
	"encoding/json"
	"testing"
	"time"
	"uuid"
)

func TestDSHChildStopReasonSurvivesReducerPostgres(t *testing.T) {
	for _, reason := range []string{"interrupted", "retries_exhausted", "end_turn"} {
		t.Run(reason, func(t *testing.T) {
			ctx, store, scope, _ := newDSHConfirmationTestStore(t, "resolved")
			code, _, err := store.GetCodeSession(ctx, scope.CodeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			primary, err := NewSessionThreadMapper(store.mapperDB).FindPrimary(ctx, scope.WorkspaceUUID, scope.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			child, err := store.CreateSessionThreadIfAbsent(ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "stop-reason-child", OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: code.SessionUUID, SessionExternalID: scope.SessionID, ParentThreadUUID: &primary.UUID, ParentThreadExternalID: &primary.ExternalID, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			publish := func(id, status, stop string) DSHPublicEvent {
				t.Helper()
				body := map[string]any{"id": id, "type": "session.thread_status_" + status, "session_thread_id": child.ExternalID}
				if stop != "" {
					body["stop_reason"] = map[string]string{"type": stop}
				}
				raw, _ := json.Marshal(body)
				return DSHPublicEvent{ID: id, Payload: raw, Events: []SessionEvent{{UUID: uuid.NewV4().String(), ExternalID: id, EventType: "session.thread_status_" + status, ThreadExternalID: &child.ExternalID, StatusThreadID: child.ExternalID, Payload: raw, CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}}}
			}
			latest := func(want string) {
				t.Helper()
				row, found, err := NewSessionEventMapper(store.mapperDB).FindLatestStatus(ctx, scope.WorkspaceUUID, scope.SessionID, child.ExternalID)
				if err != nil || !found {
					t.Fatal("missing child status", found, err)
				}
				var payload sessionStatusPayload
				if json.Unmarshal(row.Payload, &payload) != nil || payload.StopReason == nil || payload.StopReason.Type != want {
					t.Fatalf("child stop reason changed: %s want %s", row.Payload, want)
				}
			}
			idle := publish("child-stopped", "idle", reason)
			changed, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{idle})
			if err != nil {
				t.Fatal(err)
			}
			assertDSHChildStatusCopies(t, changed.Changes.Events, child.ExternalID, "session.thread_status_idle", reason)
			latest(reason)
			duplicate, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{idle})
			if err != nil || duplicate.Duplicates != 1 {
				t.Fatal("replay", err, duplicate.Duplicates)
			}
			latest(reason)
			if _, changes, err := store.FinishDSHInput(ctx, scope, "input", DSHFinish{Result: "success"}); err != nil {
				t.Fatal(err)
			} else {
				for _, event := range changes.Events {
					if event.StatusThreadID == child.ExternalID {
						t.Fatal("parent finish emitted child transition", event.EventType)
					}
				}
			}
			latest(reason)
			if _, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{publish("child-started-new-turn", "running", "")}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.PublishDSHEvents(ctx, scope, []DSHPublicEvent{publish("child-completed-new-turn", "idle", "end_turn")}); err != nil {
				t.Fatal(err)
			}
			latest("end_turn")
		})
	}
}

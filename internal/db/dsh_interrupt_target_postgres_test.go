//go:build integration

package db

import (
	"errors"
	"testing"
	"time"
	"uuid"
)

func TestDSHInterruptRejectsTerminatedTargetPostgres(t *testing.T) {
	for _, status := range []string{"terminated", "running", "idle"} {
		t.Run(status, func(t *testing.T) {
			ctx, store, scope, _ := newDSHConfirmationTestStore(t, "resolved")
			code, _, err := store.GetCodeSession(ctx, scope.CodeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			primary, err := NewSessionThreadMapper(store.mapperDB).FindPrimary(ctx, scope.WorkspaceUUID, scope.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			child, err := store.CreateSessionThreadIfAbsent(ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "interrupt-child", OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: code.SessionUUID, SessionExternalID: scope.SessionID, ParentThreadUUID: &primary.UUID, ParentThreadExternalID: &primary.ExternalID, AgentSnapshot: []byte(`{}`), Status: status, Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "interrupt-check", EventType: "user.interrupt", ThreadExternalID: &child.ExternalID, Payload: []byte(`{"type":"user.interrupt","session_thread_id":"interrupt-child"}`), CreatedAt: time.Now().UTC()}
			changes, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event}, nil)
			if status == "terminated" {
				if !errors.Is(err, ErrSessionInterruptTargetInvalid) || len(changes.Events) != 0 {
					t.Fatal("terminated interrupt accepted", err, changes)
				}
			} else if err != nil {
				t.Fatal("live interrupt rejected", err)
			}
		})
	}
}

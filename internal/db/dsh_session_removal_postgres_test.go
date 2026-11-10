//go:build integration

package db

import (
	"errors"
	"testing"
	"time"
	"uuid"
)

func TestDSHSessionRemovalRejectsRunningInputPostgres(t *testing.T) {
	for _, archive := range []bool{false, true} {
		name := "delete"
		if archive {
			name = "archive"
		}
		t.Run(name, func(t *testing.T) {
			f, _ := newDSRTurnBoundaryFixture(t)
			event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "input", EventType: "user.message", Payload: []byte(`{"type":"user.message","content":[{"type":"text","text":"work"}]}`), CreatedAt: time.Now().UTC()}
			if _, err := f.store.AppendSessionEvents(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, []SessionEvent{event}, nil); err != nil {
				t.Fatal(err)
			}
			claim, _, err := f.store.ClaimDSHInput(f.ctx, f.scope)
			if err != nil {
				t.Fatal(err)
			}
			if archive {
				_, _, err = f.store.ArchiveSession(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID)
			} else {
				_, err = f.store.DeleteSession(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID)
			}
			if !errors.Is(err, ErrInvalidState) {
				t.Fatalf("active input removal: %v", err)
			}
			session, found, err := f.store.GetSession(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID)
			if err != nil || !found || session.Status != "running" || session.ArchivedAt != nil {
				t.Fatalf("running session changed: %+v %v %v", session, found, err)
			}
			if _, _, err = f.store.FinishDSHInput(f.ctx, f.scope, claim.Event.ExternalID, DSHFinish{Result: "cancelled"}); err != nil {
				t.Fatal(err)
			}
			if archive {
				_, _, err = f.store.ArchiveSession(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID)
			} else {
				_, err = f.store.DeleteSession(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID)
			}
			if err != nil {
				t.Fatalf("completed input removal: %v", err)
			}
		})
	}
}

func TestDSHSessionRemovalAllowsUnclaimedInputPostgres(t *testing.T) {
	f, _ := newDSRTurnBoundaryFixture(t)
	event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "unclaimed", EventType: "user.message", Payload: []byte(`{"type":"user.message","content":[{"type":"text","text":"queued"}]}`), CreatedAt: time.Now().UTC()}
	if _, err := f.store.AppendSessionEvents(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, []SessionEvent{event}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DeleteSession(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID); err != nil {
		t.Fatalf("unclaimed input removal: %v", err)
	}
}

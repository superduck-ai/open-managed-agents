package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestReclaimTerminalRuntimeStopsUnstoppedSessionAndIdlesRunning(t *testing.T) {
	runtime := &sessionRuntimeStub{}
	store := &reclaimStoreStub{
		dreams: []db.Dream{{
			WorkspaceUUID:       "ws-dream",
			ExternalID:          "drm_done",
			Status:              "completed",
			InternalSessionUUID: "session-uuid",
			Outputs:             json.RawMessage(`[{"memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
		}},
		session: db.Session{ExternalID: "sesn_internal", Status: "running", WorkspaceUUID: "ws-dream"},
	}
	reclaimer := terminalRuntimeReclaimer{store: store, runtime: runtime}
	if err := reclaimer.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce() = %v", err)
	}
	if runtime.workspaceUUID != "ws-dream" || runtime.sessionUUID != "session-uuid" {
		t.Fatalf("StopSessionRuntime = (%q, %q)", runtime.workspaceUUID, runtime.sessionUUID)
	}
	if store.status != "idle" {
		t.Fatalf("session status = %q, want idle after the sandbox is gone", store.status)
	}
}

func TestReclaimTerminalRuntimeSkipsIdleSessionStatusWrite(t *testing.T) {
	runtime := &sessionRuntimeStub{}
	store := &reclaimStoreStub{
		dreams: []db.Dream{{
			WorkspaceUUID:       "ws-dream",
			ExternalID:          "drm_done",
			InternalSessionUUID: "session-uuid",
			Outputs:             json.RawMessage(`[{"memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
		}},
		session: db.Session{ExternalID: "sesn_internal", Status: "idle", WorkspaceUUID: "ws-dream"},
	}
	reclaimer := terminalRuntimeReclaimer{store: store, runtime: runtime}
	if err := reclaimer.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce() = %v", err)
	}
	if runtime.sessionUUID != "session-uuid" {
		t.Fatal("idle completed Dreams must still stop leftover Work/sandbox")
	}
	if store.status != "" {
		t.Fatalf("SetSessionStatus = %q, want no write for an already idle Session", store.status)
	}
}

func TestReclaimTerminalRuntimeSurfacesStopErrors(t *testing.T) {
	want := errors.New("kill sandbox")
	store := &reclaimStoreStub{
		dreams: []db.Dream{{
			WorkspaceUUID:       "ws-dream",
			InternalSessionUUID: "session-uuid",
			Outputs:             json.RawMessage(`[{"memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
		}},
		session: db.Session{ExternalID: "sesn_internal", Status: "running"},
	}
	reclaimer := terminalRuntimeReclaimer{
		store:   store,
		runtime: &sessionRuntimeStub{err: want},
	}
	if err := reclaimer.RunOnce(t.Context()); !errors.Is(err, want) {
		t.Fatalf("RunOnce() = %v, want %v", err, want)
	}
	if store.status != "" {
		t.Fatalf("SetSessionStatus = %q, want no idle write after a failed Kill", store.status)
	}
}

type sessionRuntimeStub struct {
	workspaceUUID string
	sessionUUID   string
	err           error
}

func (s *sessionRuntimeStub) StopSessionRuntime(_ context.Context, workspaceUUID, sessionUUID string) error {
	s.workspaceUUID = workspaceUUID
	s.sessionUUID = sessionUUID
	return s.err
}

type reclaimStoreStub struct {
	dreams  []db.Dream
	session db.Session
	status  string
}

func (s *reclaimStoreStub) ListDreamsAwaitingRuntimeReclaim(context.Context, int) ([]db.Dream, error) {
	return s.dreams, nil
}

func (s *reclaimStoreStub) GetSession(_ context.Context, _, _ string) (db.Session, bool, error) {
	return s.session, true, nil
}

func (s *reclaimStoreStub) SetSessionStatus(_ context.Context, _, _, status string) error {
	s.status = status
	return nil
}

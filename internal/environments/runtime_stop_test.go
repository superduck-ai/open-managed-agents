package environments

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

func TestStopSessionRuntimeKillsActiveSandboxThenForceStopsWork(t *testing.T) {
	providerID := "sbx_live"
	store := &runtimeStopStoreStub{
		works: []db.EnvironmentWork{{
			WorkspaceUUID:         "ws",
			EnvironmentExternalID: "env_1",
			ExternalID:            "work_1",
			SessionUUID:           "session-uuid",
			State:                 "stopping",
		}},
		sandbox: db.EnvironmentSandbox{ExternalID: "envsbx_1", ProviderSandboxID: &providerID, State: "running"},
	}
	killer := &runtimeKillerStub{}
	stopper := SessionRuntimeStopper{store: store, killer: killer}
	if err := stopper.StopSessionRuntime(t.Context(), "ws", "session-uuid"); err != nil {
		t.Fatalf("StopSessionRuntime() = %v", err)
	}
	if killer.id != providerID {
		t.Fatalf("Kill(%q), want %q", killer.id, providerID)
	}
	if store.sandboxState != "stopped" || store.workForce != true {
		t.Fatalf("sandbox/work = (%q, force=%v), want stopped+force", store.sandboxState, store.workForce)
	}
}

func TestStopSessionRuntimeForceStopsWorkWhenSandboxAlreadyGone(t *testing.T) {
	store := &runtimeStopStoreStub{
		works: []db.EnvironmentWork{{
			WorkspaceUUID: "ws", EnvironmentExternalID: "env_1", ExternalID: "work_1", State: "stopping",
		}},
		sandboxErr: db.ErrNotFound,
	}
	stopper := SessionRuntimeStopper{store: store, killer: &runtimeKillerStub{}}
	if err := stopper.StopSessionRuntime(t.Context(), "ws", "session-uuid"); err != nil {
		t.Fatalf("StopSessionRuntime() = %v", err)
	}
	if store.workForce != true {
		t.Fatal("Work must still be force-stopped when no sandbox remains")
	}
}

func TestStopSessionRuntimeTreatsProviderNotFoundAsStopped(t *testing.T) {
	providerID := "sbx_gone"
	store := &runtimeStopStoreStub{
		works: []db.EnvironmentWork{{
			WorkspaceUUID: "ws", EnvironmentExternalID: "env_1", ExternalID: "work_1", State: "stopping",
		}},
		sandbox: db.EnvironmentSandbox{ExternalID: "envsbx_1", ProviderSandboxID: &providerID, State: "running"},
	}
	stopper := SessionRuntimeStopper{store: store, killer: &runtimeKillerStub{err: sandboxruntime.ErrSandboxNotFound}}
	if err := stopper.StopSessionRuntime(t.Context(), "ws", "session-uuid"); err != nil {
		t.Fatalf("StopSessionRuntime() = %v", err)
	}
	if store.sandboxState != "stopped" || store.workForce != true {
		t.Fatalf("sandbox/work = (%q, force=%v), want stopped+force after provider 404", store.sandboxState, store.workForce)
	}
}

func TestStopSessionRuntimeDoesNotMarkStoppedWhenKillFails(t *testing.T) {
	providerID := "sbx_live"
	store := &runtimeStopStoreStub{
		works: []db.EnvironmentWork{{
			WorkspaceUUID: "ws", EnvironmentExternalID: "env_1", ExternalID: "work_1", State: "stopping",
		}},
		sandbox: db.EnvironmentSandbox{ExternalID: "envsbx_1", ProviderSandboxID: &providerID, State: "running"},
	}
	want := errors.New("e2b down")
	stopper := SessionRuntimeStopper{store: store, killer: &runtimeKillerStub{err: want}}
	if err := stopper.StopSessionRuntime(t.Context(), "ws", "session-uuid"); !errors.Is(err, want) {
		t.Fatalf("StopSessionRuntime() = %v, want %v", err, want)
	}
	if store.workForce {
		t.Fatal("must not force-stop Work after a failed Kill")
	}
	if store.sandboxState != "stopping" {
		t.Fatalf("sandbox state = %q, want stopping so the next tick can retry Kill", store.sandboxState)
	}
}

func TestStopSessionRuntimeIsNoopWithoutUnstoppedWork(t *testing.T) {
	killer := &runtimeKillerStub{}
	stopper := SessionRuntimeStopper{store: &runtimeStopStoreStub{}, killer: killer}
	if err := stopper.StopSessionRuntime(t.Context(), "ws", "session-uuid"); err != nil {
		t.Fatalf("StopSessionRuntime() = %v", err)
	}
	if killer.id != "" {
		t.Fatalf("Kill(%q), want none", killer.id)
	}
}

type runtimeKillerStub struct {
	id  string
	err error
}

func (k *runtimeKillerStub) Kill(_ context.Context, sandboxID string) error {
	k.id = sandboxID
	return k.err
}

type runtimeStopStoreStub struct {
	works        []db.EnvironmentWork
	sandbox      db.EnvironmentSandbox
	sandboxErr   error
	sandboxState string
	workForce    bool
}

func (s *runtimeStopStoreStub) ListUnstoppedEnvironmentWorkBySession(context.Context, string, string) ([]db.EnvironmentWork, error) {
	return s.works, nil
}

func (s *runtimeStopStoreStub) GetActiveEnvironmentSandboxForWork(context.Context, string, string, string) (db.EnvironmentSandbox, error) {
	if s.sandboxErr != nil {
		return db.EnvironmentSandbox{}, s.sandboxErr
	}
	return s.sandbox, nil
}

func (s *runtimeStopStoreStub) UpdateEnvironmentSandboxState(_ context.Context, _, _ string, state string, _ *string, _ *string, _ *time.Time) error {
	s.sandboxState = state
	return nil
}

func (s *runtimeStopStoreStub) StopEnvironmentWork(_ context.Context, _, _, _ string, force bool) (db.EnvironmentWork, error) {
	s.workForce = force
	return db.EnvironmentWork{State: "stopped"}, nil
}

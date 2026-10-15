package tests

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
)

func TestSessionArchiveSandboxRollbackAndIsolation(t *testing.T) {
	t.Run("archive rollback", func(t *testing.T) {
		f := newSandboxLifecycleFixture(t)
		f.exec(t, `ALTER TABLE environment_sandboxes ADD CONSTRAINT reject_archive_stop CHECK (stop_reason IS DISTINCT FROM 'session_archived')`)
		defer f.exec(t, `ALTER TABLE environment_sandboxes DROP CONSTRAINT reject_archive_stop`)
		if _, _, err := f.app.db.ArchiveSession(context.Background(), f.session.WorkspaceUUID, f.session.ExternalID); err == nil {
			t.Fatal("archive should fail when sandbox cleanup cannot be recorded")
		}
		current := mustSessionRecord(t, f.app, f.session.ExternalID)
		if current.ArchivedAt != nil || current.Status != f.session.Status {
			t.Fatalf("archive escaped rollback: %+v", current)
		}
		code, err := getCodeSession(f.app, context.Background(), f.code.ExternalID)
		if err != nil || code.Status != f.code.Status {
			t.Fatalf("worker escaped rollback: %+v, %v", code, err)
		}
		if work, sandbox := f.state(t); work != "active" || sandbox != "running" {
			t.Fatalf("state escaped rollback: %s/%s", work, sandbox)
		}
	})
	t.Run("wrong workspace", func(t *testing.T) {
		f := newSandboxLifecycleFixture(t)
		if _, _, err := f.app.db.ArchiveSession(context.Background(), uuid.NewV4().String(), f.session.ExternalID); err == nil {
			t.Fatal("cross workspace archive succeeded")
		}
		if work, sandbox := f.state(t); work != "active" || sandbox != "running" {
			t.Fatalf("cross workspace state changed: %s/%s", work, sandbox)
		}
	})
	t.Run("self hosted", func(t *testing.T) {
		f := newSandboxLifecycleFixture(t)
		f.exec(t, `UPDATE environments SET config = '{"type":"self_hosted"}' WHERE uuid = $1`, f.session.EnvironmentUUID)
		archiveSession(t, f.app, f.session.ExternalID)
		if work, sandbox := f.state(t); work != "active" || sandbox != "running" {
			t.Fatalf("self hosted sandbox changed: %s/%s", work, sandbox)
		}
	})
}

func TestSessionArchiveWebhookAndSandboxCleanup(t *testing.T) {
	t.Run("sandbox rollback does not notify", func(t *testing.T) {
		app, _, _ := newEventSubscription(t, []string{"session.status_terminated"})
		f := newSandboxLifecycleFixtureWithApp(t, app)
		f.exec(t, `ALTER TABLE environment_sandboxes ADD CONSTRAINT reject_archive_webhook_stop CHECK (stop_reason IS DISTINCT FROM 'session_archived')`)
		defer f.exec(t, `ALTER TABLE environment_sandboxes DROP CONSTRAINT reject_archive_webhook_stop`)
		response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+f.session.ExternalID+"/archive?beta=true", nil, defaultTestKey, true)
		assertError(t, response, http.StatusInternalServerError, "api_error")
		assertWebhookCount(t, app, "session.status_terminated", f.session.ExternalID, 0)
		current := mustSessionRecord(t, app, f.session.ExternalID)
		if current.ArchivedAt != nil || current.Status != f.session.Status {
			t.Fatalf("archive escaped sandbox rollback: %+v", current)
		}
		if work, sandbox := f.state(t); work != "active" || sandbox != "running" {
			t.Fatalf("sandbox rollback state = %s/%s", work, sandbox)
		}
	})
	t.Run("repeat archive preserves notification and timestamps", func(t *testing.T) {
		app, _, _ := newEventSubscription(t, []string{"session.status_terminated"})
		f := newSandboxLifecycleFixtureWithApp(t, app)
		archiveSession(t, app, f.session.ExternalID)
		first := mustSessionRecord(t, app, f.session.ExternalID)
		if first.ArchivedAt == nil {
			t.Fatal("session was not archived")
		}
		archiveSession(t, app, f.session.ExternalID)
		second := mustSessionRecord(t, app, f.session.ExternalID)
		if second.ArchivedAt == nil || !first.ArchivedAt.Equal(*second.ArchivedAt) || !first.UpdatedAt.Equal(second.UpdatedAt) {
			t.Fatal("repeat archive changed session timestamps")
		}
		assertWebhookCount(t, app, "session.status_terminated", f.session.ExternalID, 1)
		if work, sandbox := f.state(t); work != "stopped" || sandbox != "stopping" {
			t.Fatalf("archive cleanup state = %s/%s", work, sandbox)
		}
		code, err := getCodeSession(app, t.Context(), f.code.ExternalID)
		if err != nil || code.Status != "terminated" {
			t.Fatalf("archive kept a live worker: %+v, %v", code, err)
		}
	})
}

func TestSessionArchiveSandboxDeletionRetriesAndPreservesHistory(t *testing.T) {
	f := newSandboxLifecycleFixture(t)
	ctx := context.Background()
	archiveSession(t, f.app, f.session.ExternalID)
	archiveSession(t, f.app, f.session.ExternalID)
	if work, sandbox := f.state(t); work != "stopped" || sandbox != "stopping" {
		t.Fatalf("archive state = %s/%s", work, sandbox)
	}
	candidates, err := f.app.db.ListSandboxReclaimCandidates(ctx, time.Now(), "", 100, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range candidates {
		if target.SandboxUUID == f.target.SandboxUUID {
			found = target.Reclaiming
		}
	}
	if !found {
		t.Fatal("disabled sweep omitted archived sandbox")
	}
	var attempts atomic.Int32
	provider := newLifecycleProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sandboxes/"+f.target.ProviderSandboxID {
			t.Errorf("wrong deletion target: %s", r.URL.Path)
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	lifecycle := environments.NewSandboxLifecycle(f.app.db, provider, config.SandboxLifecycleConfig{DryRun: true}, nil)
	if err := lifecycle.Reclaim(ctx, f.target); err == nil {
		t.Fatal("provider failure must be retried")
	}
	if _, sandbox := f.state(t); sandbox != "stopping" {
		t.Fatalf("lost retry state: %s", sandbox)
	}
	for range 2 {
		if err := lifecycle.Reclaim(ctx, f.target); err != nil {
			t.Fatal(err)
		}
	}
	if attempts.Load() != 2 {
		t.Fatalf("deletion attempts = %d, want 2", attempts.Load())
	}
	if work, sandbox := f.state(t); work != "stopped" || sandbox != "stopped" {
		t.Fatalf("completion state = %s/%s", work, sandbox)
	}
	current := mustSessionRecord(t, f.app, f.session.ExternalID)
	if current.ArchivedAt == nil || current.DeletedAt != nil {
		t.Fatal("archive did not preserve Session")
	}
	events, _, err := f.app.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{WorkspaceUUID: f.session.WorkspaceUUID, SessionExternalID: f.session.ExternalID, Limit: 100})
	if err != nil || len(events) == 0 {
		t.Fatalf("archive lost history: %d, %v", len(events), err)
	}
	if queued, err := f.app.db.ScheduleEnvironmentSandboxRecoveryForCodeSession(ctx, f.code.ExternalID, f.target.ProviderSandboxID, nil, ""); err != nil || queued {
		t.Fatalf("archived sandbox recovered: %t, %v", queued, err)
	}
}

func TestSessionArchiveSandboxBeforeWorkerAndLateAllocation(t *testing.T) {
	for _, late := range []bool{false, true} {
		name := "before worker"
		if late {
			name = "provider allocation finishes after archive"
		}
		t.Run(name, func(t *testing.T) {
			f := newSandboxLifecycleFixture(t)
			f.exec(t, `DELETE FROM code_sessions WHERE uuid = $1`, f.code.UUID)
			if late {
				f.exec(t, `UPDATE environment_sandboxes SET state = 'creating', provider_sandbox_id = NULL WHERE uuid = $1`, f.target.SandboxUUID)
			}
			archiveSession(t, f.app, f.session.ExternalID)
			var externalID string
			if err := f.app.pool.QueryRow(context.Background(), `SELECT external_id FROM environment_sandboxes WHERE uuid = $1`, f.target.SandboxUUID).Scan(&externalID); err != nil {
				t.Fatal(err)
			}
			if late {
				if err := f.app.db.UpdateEnvironmentSandboxState(context.Background(), f.session.WorkspaceUUID, externalID, "running", &f.target.ProviderSandboxID, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			provider := newLifecycleProvider(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusNoContent)
			})
			lifecycle := environments.NewSandboxLifecycle(f.app.db, provider, config.SandboxLifecycleConfig{}, nil)
			if err := lifecycle.Reclaim(context.Background(), f.target); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatalf("provider calls = %d, want 1", calls.Load())
			}
			if work, sandbox := f.state(t); work != "stopped" || sandbox != "stopped" {
				t.Fatalf("state = %s/%s", work, sandbox)
			}
			if err := f.app.db.UpdateEnvironmentSandboxState(context.Background(), f.session.WorkspaceUUID, externalID, "running", &f.target.ProviderSandboxID, nil, nil); err != nil {
				t.Fatal(err)
			}
			if _, sandbox := f.state(t); sandbox != "stopped" {
				t.Fatalf("completed archive cleanup was overwritten: %s", sandbox)
			}
		})
	}
}

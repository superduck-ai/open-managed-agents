//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
)

func TestSessionDeleteSandboxReclamation(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.URL = deletedSandboxDatabase(t, cfg.Database.URL)
	app := newTestAppWithStore(t, &cfg, newFakeStore("delete-sandbox-isolated"))
	t.Cleanup(app.close)
	t.Run("transaction rollback", func(t *testing.T) {
		f := newSandboxLifecycleFixtureWithApp(t, app)
		f.exec(t, `ALTER TABLE environment_sandboxes ADD CONSTRAINT reject_delete_stop CHECK (stop_reason IS DISTINCT FROM 'session_deleted')`)
		defer f.exec(t, `ALTER TABLE environment_sandboxes DROP CONSTRAINT reject_delete_stop`)
		if _, err := app.db.DeleteSession(context.Background(), f.session.WorkspaceUUID, f.session.ExternalID); err == nil {
			t.Fatal("delete escaped failed durable reclamation claim")
		}
		current := mustSessionRecord(t, app, f.session.ExternalID)
		if current.DeletedAt != nil {
			t.Fatal("failed claim soft-deleted Session")
		}
		if work, sandbox := f.state(t); work != "active" || sandbox != "running" {
			t.Fatalf("rollback changed resources: %s/%s", work, sandbox)
		}
	})
	t.Run("API enqueue retry immutable target and retained resources", func(t *testing.T) {
		f := newSandboxLifecycleFixtureWithApp(t, app)
		unrelated := newSandboxLifecycleFixtureWithApp(t, app)
		var attempts atomic.Int32
		called := make(chan string, 2)
		provider := newLifecycleProvider(t, func(w http.ResponseWriter, r *http.Request) {
			called <- strings.TrimPrefix(r.URL.Path, "/sandboxes/")
			if attempts.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		})
		lifecycle := environments.NewSandboxLifecycle(app.db, provider, config.SandboxLifecycleConfig{DryRun: true}, nil)
		serveArchiveWithLifecycle(t, app, lifecycle)
		deleteSession(t, app, f.session.ExternalID)
		select {
		case id := <-called:
			if id != f.target.ProviderSandboxID {
				t.Fatal("deleted replacement or foreign provider", id)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("HTTP delete did not enqueue exact associated sandbox")
		}
		response := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+f.session.ExternalID+"?beta=true", nil, defaultTestKey, true)
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatal("deleted Session remains visible", response.StatusCode)
		}
		if _, sandbox := f.state(t); sandbox != "stopping" {
			t.Fatal("failed provider deletion lost durable retry", sandbox)
		}
		targets, err := app.db.ListSandboxReclaimCandidates(context.Background(), time.Now(), "", 100, false)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, target := range targets {
			found = found || target.SandboxUUID == f.target.SandboxUUID && target.ProviderSandboxID == f.target.ProviderSandboxID && target.Reclaiming
		}
		if !found {
			t.Fatal("deleted target cannot be recovered by durable sweep")
		}
		if err := lifecycle.Reclaim(context.Background(), f.target); err != nil {
			t.Fatal(err)
		}
		if err := lifecycle.Reclaim(context.Background(), f.target); err != nil {
			t.Fatal(err)
		}
		if attempts.Load() != 2 {
			t.Fatal("completed deletion not idempotent", attempts.Load())
		}
		if work, sandbox := f.state(t); work != "stopped" || sandbox != "stopped" {
			t.Fatalf("deleted resource completion %s/%s", work, sandbox)
		}
		if work, sandbox := unrelated.state(t); work != "active" || sandbox != "running" {
			t.Fatalf("unrelated Session changed %s/%s", work, sandbox)
		}
		for _, path := range []string{"/v1/environments/" + f.session.EnvironmentExternalID + "?beta=true", "/v1/agents/" + f.session.AgentExternalID + "?beta=true"} {
			response := doSessionRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("shared resource removed: %s %d", path, response.StatusCode)
			}
		}
	})
}

func deletedSandboxDatabase(t *testing.T, source string) string {
	t.Helper()
	target, err := url.Parse(source)
	if err != nil || (target.Hostname() != "localhost" && target.Hostname() != "127.0.0.1") {
		t.Fatal("isolated delete acceptance requires local PostgreSQL")
	}
	target.Path = "/postgres"
	maintenance, err := sql.Open("pgx", target.String())
	if err != nil {
		t.Fatal(err)
	}
	name := "oma_deleted_sandbox_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := maintenance.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		maintenance.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := maintenance.ExecContext(ctx, "DROP DATABASE "+name); err != nil {
			t.Errorf("cleanup owned delete acceptance database: %v", err)
		}
		maintenance.Close()
	})
	target.Path = "/" + name
	return target.String()
}

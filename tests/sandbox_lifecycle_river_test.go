package tests

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
)

func TestSandboxLifecycleDurableScheduleDispatchesReclaim(t *testing.T) {
	t.Run("idle", func(t *testing.T) { testSandboxLifecycleSchedule(t, false) })
	t.Run("archived with idle cleanup disabled", func(t *testing.T) { testSandboxLifecycleSchedule(t, true) })
}

func testSandboxLifecycleSchedule(t *testing.T, archived bool) {
	f := newSandboxLifecycleFixture(t)
	if archived {
		archiveSession(t, f.app, f.session.ExternalID)
	}
	ctx := context.Background()
	if err := riverjobs.Migrate(ctx, f.app.db, nil); err != nil {
		t.Fatal(err)
	}
	calls := make(chan string, 100)
	killer := newLifecycleProvider(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case calls <- strings.TrimPrefix(r.URL.Path, "/sandboxes/"):
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	lifecycle := environments.NewSandboxLifecycle(f.app.db, killer, config.SandboxLifecycleConfig{Enabled: !archived, DryRun: archived, IdleTimeout: 24 * time.Hour}, nil)
	workers := river.NewWorkers()
	lifecycle.Register(workers)
	const sweepQueue = "sandbox_lifecycle_test_sweep"
	client, err := riverjobs.NewClient(f.app.db, nil, workers, map[string]river.QueueConfig{
		sweepQueue:                         {MaxWorkers: 1, FetchPollInterval: time.Hour},
		environments.SandboxLifecycleQueue: {MaxWorkers: 4, FetchPollInterval: time.Hour},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Configure(ctx, client); err != nil {
		t.Fatal(err)
	}
	before, err := client.DurablePeriodicJobGet(ctx, "sandbox_lifecycle_sweep")
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Configure(ctx, client); err != nil {
		t.Fatal(err)
	}
	after, err := client.DurablePeriodicJobGet(ctx, "sandbox_lifecycle_sweep")
	if err != nil || !before.NextRunAt.Equal(after.NextRunAt) {
		t.Fatalf("startup changed existing schedule: before=%+v after=%+v err=%v", before, after, err)
	}
	scheduleID := "lifecycle-test-" + uuid.NewV4().String()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := client.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
		defer stopCancel()
		if err := client.Stop(stopCtx); err != nil {
			t.Error(err)
		}
		_, _ = client.DurablePeriodicJobDelete(ctx, scheduleID)
		_, _ = client.DurablePeriodicJobDelete(ctx, "sandbox_lifecycle_sweep")
	}()
	assertRiverListenerConnected(t, f.app)
	_, err = client.DurablePeriodicJobUpsert(ctx, &river.DurablePeriodicJobUpsertOpts{
		ID: scheduleID, Kind: "sandbox_lifecycle_sweep", Queue: sweepQueue,
		Schedule: &river.DurablePeriodicJobSchedule{NextRunAt: time.Now().Add(-time.Second)},
	})
	if err != nil {
		t.Fatal(err)
	}
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case id := <-calls:
			if id == f.target.ProviderSandboxID {
				return
			}
		case <-timeout.C:
			logSandboxLifecycleState(t, f)
			t.Fatal("durable sweep did not dispatch idle sandbox reclamation")
		}
	}
}

func logSandboxLifecycleState(t *testing.T, f sandboxLifecycleFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var state string
	if err := f.app.pool.QueryRow(ctx, `SELECT json_build_object(
        'session_status', s.status, 'session_deleted', s.deleted_at IS NOT NULL,
        'session_archived', s.archived_at IS NOT NULL, 'worker_status', cs.worker_status,
        'code_status', cs.status, 'idle_since', cs.idle_since, 'environment_type', e.config->>'type',
        'work_state', w.state, 'sandbox_state', b.state)
        FROM environment_sandboxes b JOIN environment_work w ON w.uuid=b.work_uuid
        JOIN sessions s ON s.uuid=w.session_uuid JOIN code_sessions cs ON cs.session_uuid=s.uuid
        JOIN environments e ON e.uuid=s.environment_uuid WHERE b.uuid=$1`, f.target.SandboxUUID).Scan(&state); err != nil {
		t.Logf("sandbox state unavailable: %v", err)
	} else {
		t.Logf("sandbox state: %s", state)
	}
	var jobs string
	if err := f.app.pool.QueryRow(ctx, `SELECT COALESCE(json_agg(state), '[]'::json) FROM public.river_job
        WHERE kind='sandbox_reclaim' AND args->>'sandbox_uuid'=$1`, f.target.SandboxUUID).Scan(&jobs); err != nil {
		t.Logf("reclaim job state unavailable: %v", err)
	} else {
		t.Logf("reclaim jobs: %s", jobs)
	}
}

func assertRiverListenerConnected(t *testing.T, app *testApp) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var listening bool
		if err := app.pool.QueryRow(ctx, `SELECT EXISTS (
            SELECT 1 FROM pg_stat_activity WHERE datname = current_database()
                AND state = 'idle' AND query LIKE 'LISTEN "public.river_%'
        )`).Scan(&listening); err != nil {
			t.Fatal(err)
		}
		if listening {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("River started without a PostgreSQL LISTEN connection")
		}
	}
}

func TestSessionArchiveImmediatelyReclaimsSandbox(t *testing.T) {
	t.Run("delete failure preserves archive and queues retry", func(t *testing.T) {
		f := newSandboxLifecycleFixture(t)
		var calls atomic.Int32
		provider := newLifecycleProvider(t, func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		})
		lifecycle := environments.NewSandboxLifecycle(f.app.db, provider, config.SandboxLifecycleConfig{}, nil)
		workers := river.NewWorkers()
		lifecycle.Register(workers)
		client, err := riverjobs.NewClient(f.app.db, nil, workers, map[string]river.QueueConfig{
			environments.SandboxLifecycleQueue: {MaxWorkers: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := lifecycle.Configure(context.Background(), client); err != nil {
			t.Fatal(err)
		}
		serveArchiveWithLifecycle(t, f.app, lifecycle)
		archiveSession(t, f.app, f.session.ExternalID)
		deadline := time.Now().Add(5 * time.Second)
		for {
			var queued int
			if err := f.app.pool.QueryRow(context.Background(), `SELECT count(*) FROM public.river_job WHERE kind = 'sandbox_reclaim' AND args->>'sandbox_uuid' = $1 AND state = 'available'`, f.target.SandboxUUID).Scan(&queued); err != nil {
				t.Fatal(err)
			}
			if queued == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("failed deletion did not queue retry")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if current := mustSessionRecord(t, f.app, f.session.ExternalID); current.ArchivedAt == nil {
			t.Fatal("deletion failure reversed archive")
		}
		if work, sandbox := f.state(t); work != "stopped" || sandbox != "stopping" {
			t.Fatalf("deletion failure lost durable intent: %s/%s", work, sandbox)
		}
		archiveSession(t, f.app, f.session.ExternalID)
		waitForArchivedSandboxDeletion(t, f)
	})
	t.Run("bounded deletion continues after archive response", func(t *testing.T) {
		f := newSandboxLifecycleFixture(t)
		fixtures := []sandboxLifecycleFixture{f}
		for range 4 {
			fixtures = append(fixtures, newSandboxLifecycleFixtureWithApp(t, f.app))
		}
		calls := make(chan string, 10)
		release := make(chan struct{}, 5)
		defer close(release)
		provider := newLifecycleProvider(t, func(w http.ResponseWriter, r *http.Request) {
			calls <- strings.TrimPrefix(r.URL.Path, "/sandboxes/")
			select {
			case <-release:
				w.WriteHeader(http.StatusNoContent)
			case <-r.Context().Done():
			}
		})
		lifecycle := environments.NewSandboxLifecycle(f.app.db, provider, config.SandboxLifecycleConfig{DryRun: true, IdleTimeout: 24 * time.Hour}, nil)
		workers := river.NewWorkers()
		lifecycle.Register(workers)
		client, err := riverjobs.NewClient(f.app.db, nil, workers, map[string]river.QueueConfig{
			environments.SandboxLifecycleQueue: {MaxWorkers: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := lifecycle.Configure(context.Background(), client); err != nil {
			t.Fatal(err)
		}
		serveArchiveWithLifecycle(t, f.app, lifecycle)
		for i, fixture := range fixtures {
			archiveSession(t, f.app, fixture.session.ExternalID)
			if i < 4 {
				select {
				case id := <-calls:
					if id != fixture.target.ProviderSandboxID {
						t.Fatalf("deleted wrong sandbox: %s", id)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("archive did not start deletion")
				}
			}
		}
		var queued int
		if err := f.app.pool.QueryRow(context.Background(), `SELECT count(*) FROM public.river_job WHERE kind = 'sandbox_reclaim' AND args->>'sandbox_uuid' = $1 AND state = 'available'`, fixtures[4].target.SandboxUUID).Scan(&queued); err != nil || queued != 1 {
			t.Fatalf("capacity fallback: queued=%d err=%v", queued, err)
		}
		if len(calls) != 0 {
			t.Fatal("deletion exceeded four concurrent tasks")
		}
		for range 4 {
			release <- struct{}{}
		}
		for _, fixture := range fixtures[:4] {
			waitForArchivedSandboxDeletion(t, fixture)
		}
		release <- struct{}{}
		if err := lifecycle.Reclaim(context.Background(), fixtures[4].target); err != nil {
			t.Fatal(err)
		}
		waitForArchivedSandboxDeletion(t, fixtures[4])
	})
}

func waitForArchivedSandboxDeletion(t *testing.T, f sandboxLifecycleFixture) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, sandbox := f.state(t); sandbox == "stopped" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("archived sandbox deletion did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSessionArchiveConcurrentSandboxReclamation(t *testing.T) {
	f := newSandboxLifecycleFixture(t)
	archiveSession(t, f.app, f.session.ExternalID)
	var calls atomic.Int32
	bothDeleting := make(chan struct{})
	provider := newLifecycleProvider(t, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 2 {
			close(bothDeleting)
		}
		select {
		case <-bothDeleting:
		case <-r.Context().Done():
			return
		}
		if call == 1 {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	completed := make(chan struct{}, 2)
	logger := slog.New(archiveCompletionHandler{
		Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), completed: completed,
	})
	lifecycle := environments.NewSandboxLifecycle(f.app.db, provider, config.SandboxLifecycleConfig{}, logger)
	results := make(chan error, 2)
	go func() { results <- lifecycle.EnqueueArchivedSession(context.Background(), f.session) }()
	go func() { results <- lifecycle.Reclaim(context.Background(), f.target) }()
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent deletion did not converge")
		}
	}
	for range 2 {
		select {
		case <-completed:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent reclamation did not finish database writes")
		}
	}
	if _, sandbox := f.state(t); sandbox != "stopped" {
		t.Fatalf("concurrent deletion state = %s", sandbox)
	}
}

type archiveCompletionHandler struct {
	slog.Handler
	completed chan struct{}
}

func (h archiveCompletionHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Message == "sandbox reclaimed" || record.Message == "sandbox reclamation completion skipped" {
		h.completed <- struct{}{}
	}
	return nil
}

func serveArchiveWithLifecycle(t *testing.T, app *testApp, lifecycle *environments.SandboxLifecycle) {
	t.Helper()
	server := httptest.NewServer(api.NewServer(api.ServerDeps{
		Config: app.cfg, DB: app.db, Deployments: app.deployments, ObjectStore: app.store,
		PlatformStore: app.sessions, CodeSessionCredentials: app.credentials,
		SandboxTimeoutExtender: app.sandboxTimeouts, WorkerEventBroker: app.workerEvents,
		FilestoreCredentials: app.filestoreCredentials, VaultSecrets: app.vaultSecrets,
		SandboxLifecycle: lifecycle,
	}))
	previousURL, previousClient := app.baseURL, app.client
	app.baseURL, app.client = server.URL, server.Client()
	t.Cleanup(func() {
		app.baseURL, app.client = previousURL, previousClient
		server.Close()
	})
}

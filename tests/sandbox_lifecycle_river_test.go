package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
)

func TestSandboxLifecycleDurableScheduleDispatchesReclaim(t *testing.T) {
	f := newSandboxLifecycleFixture(t)
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
	lifecycle := environments.NewSandboxLifecycle(f.app.db, killer, config.SandboxLifecycleConfig{Enabled: true, IdleTimeout: 24 * time.Hour}, nil)
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

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

type sandboxLifecycleFixture struct {
	app     *testApp
	code    db.CodeSession
	session db.Session
	target  db.SandboxReclaimTarget
	epoch   string
}

func newSandboxLifecycleFixture(t *testing.T) sandboxLifecycleFixture {
	t.Helper()
	app := newTestAppWithStore(t, nil, newFakeStore("sandbox-lifecycle"))
	t.Cleanup(app.close)
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"lifecycle-agent"}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	env := createEnvironment(t, app, `{"name":"lifecycle-environment"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, env.ID) })
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	t.Cleanup(func() { deleteSession(t, app, session.ID) })
	codeID := launchLocalCodeSession(t, app, session.ID)
	epoch := registerCodeSessionWorker(t, app, codeID)
	putCodeSessionWorkerState(t, app, codeID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	code, err := getCodeSession(app, context.Background(), codeID)
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := app.db.GetSession(context.Background(), code.WorkspaceUUID, session.ID)
	if err != nil || !found {
		t.Fatalf("get session: found=%t err=%v", found, err)
	}
	sandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(context.Background(), codeID)
	if err != nil {
		t.Fatal(err)
	}
	f := sandboxLifecycleFixture{app: app, code: code, session: record, epoch: epoch, target: db.SandboxReclaimTarget{
		OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, SandboxUUID: sandbox.UUID, ProviderSandboxID: *sandbox.ProviderSandboxID}}
	f.exec(t, `UPDATE code_session_inbound_events SET delivery_status = 'processed', processed_at = NOW() WHERE code_session_uuid = $1`, code.UUID)
	f.exec(t, `UPDATE code_sessions SET idle_since = NOW() - interval '25 hours' WHERE uuid = $1`, code.UUID)
	return f
}

func (f sandboxLifecycleFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.app.pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}
func (f sandboxLifecycleFixture) state(t *testing.T) (string, string) {
	t.Helper()
	var workState, sandbox string
	if err := f.app.pool.QueryRow(context.Background(), `SELECT work.state, s.state FROM environment_sandboxes s JOIN environment_work work ON work.uuid = s.work_uuid WHERE s.uuid = $1`, f.target.SandboxUUID).Scan(&workState, &sandbox); err != nil {
		t.Fatal(err)
	}
	return workState, sandbox
}

func TestSandboxReclamationRejectsUnsafeCandidates(t *testing.T) {
	cases := []struct{ name, query string }{
		{"running", `UPDATE code_sessions SET worker_status = 'running' WHERE uuid = $1`},
		{"requires action", `UPDATE code_sessions SET worker_status = 'requires_action' WHERE uuid = $1`},
		{"recent idle", `UPDATE code_sessions SET idle_since = NOW() WHERE uuid = $1`},
		{"input reset idle clock", `UPDATE code_sessions SET idle_since = NULL WHERE uuid = $1`},
		{"terminated", `UPDATE code_sessions SET status = 'terminated' WHERE uuid = $1`},
		{"archived session", `UPDATE sessions SET archived_at = NOW() WHERE uuid = (SELECT session_uuid FROM code_sessions WHERE uuid = $1)`},
		{"self hosted", `UPDATE environments SET config = '{"type":"self_hosted"}' WHERE uuid = (SELECT environment_uuid FROM code_sessions WHERE uuid = $1)`},
		{"queued input", `UPDATE code_session_inbound_events SET delivery_status = 'queued' WHERE code_session_uuid = $1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSandboxLifecycleFixture(t)
			if tc.name == "queued input" {
				// Keep an old idle timestamp so the inbound queue itself must reject the claim.
				sendSessionEvents(t, f.app, f.session.ExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"still pending"}]}]}`, defaultTestKey)
			}
			f.exec(t, tc.query, f.code.UUID)
			if tc.name == "queued input" {
				f.exec(t, `UPDATE code_sessions SET idle_since = NOW() - interval '25 hours' WHERE uuid = $1`, f.code.UUID)
			}
			_, ok, err := f.app.db.BeginSandboxReclamation(context.Background(), f.target, time.Now().Add(-24*time.Hour), true)
			if err != nil || ok {
				t.Fatalf("unsafe claim = %t, %v", ok, err)
			}
			if workState, sandbox := f.state(t); workState != "active" || sandbox != "running" {
				t.Fatalf("state = %s/%s", workState, sandbox)
			}
		})
	}
	t.Run("wrong tenant", func(t *testing.T) {
		f := newSandboxLifecycleFixture(t)
		target := f.target
		target.WorkspaceUUID = uuid.NewV4().String()
		_, ok, err := f.app.db.BeginSandboxReclamation(context.Background(), target, time.Now(), true)
		if err != nil || ok {
			t.Fatalf("cross tenant claim = %t, %v", ok, err)
		}
	})
}

func newLifecycleProvider(t *testing.T, handler http.HandlerFunc) *e2bruntime.E2BProvider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || !strings.HasPrefix(r.URL.Path, "/sandboxes/") {
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return e2bruntime.NewProvider(config.E2BConfig{
		APIKey: "e2b_0000000000000000000000000000000000000000", APIURL: server.URL, RequestTimeout: 5 * time.Second,
	})
}

func TestSandboxReclamationRetriesDeletionAndWakesAfterConcurrentInput(t *testing.T) {
	f := newSandboxLifecycleFixture(t)
	ctx := context.Background()
	calls := make(chan string, 10)
	deleting := make(chan struct{})
	finishDelete := make(chan struct{}, 1)
	defer close(finishDelete)
	var attempts atomic.Int32
	killer := newLifecycleProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls <- strings.TrimPrefix(r.URL.Path, "/sandboxes/")
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if attempt == 2 {
			close(deleting)
			select {
			case <-finishDelete:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	lifecycle := environments.NewSandboxLifecycle(f.app.db, killer, config.SandboxLifecycleConfig{Enabled: true, IdleTimeout: 24 * time.Hour}, nil)
	if err := lifecycle.Reclaim(ctx, f.target); err == nil {
		t.Fatal("expected provider failure")
	}
	if workState, sandbox := f.state(t); workState != "active" || sandbox != "stopping" {
		t.Fatalf("state = %s/%s", workState, sandbox)
	}
	revoked, err := getCodeSession(f.app, ctx, f.code.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.CurrentWorkerEpoch <= f.code.CurrentWorkerEpoch || revoked.WorkerLeaseExpiresAt != nil {
		t.Fatal("old worker not fenced")
	}
	if _, err := f.app.db.RecordCodeSessionWorkerHeartbeat(ctx, f.code.ExternalID, f.code.CurrentWorkerEpoch, time.Minute, 10*time.Second); !errors.Is(err, db.ErrWorkerNotRegistered) {
		t.Fatalf("old heartbeat = %v", err)
	}
	// Disabling the policy stops new claims but must finish a deletion that was
	// already committed before the configuration changed.
	lifecycle = environments.NewSandboxLifecycle(f.app.db, killer, config.SandboxLifecycleConfig{IdleTimeout: 24 * time.Hour}, nil)
	result := make(chan error, 1)
	go func() { result <- lifecycle.Reclaim(ctx, f.target) }()
	select {
	case <-deleting:
	case <-time.After(5 * time.Second):
		t.Fatal("provider deletion did not start")
	}
	sendSessionEvents(t, f.app, f.session.ExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"arrived during deletion"}]}]}`, defaultTestKey)
	if workState, _ := f.state(t); workState != "active" {
		t.Fatalf("premature recovery: %s", workState)
	}
	if calls := f.app.sandboxTimeouts.snapshotCalls(); len(calls) != 0 {
		t.Fatalf("resumed sandbox during deletion: %+v", calls)
	}
	finishDelete <- struct{}{}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if workState, sandbox := f.state(t); workState != "queued" || sandbox != "failed" {
		t.Fatalf("state = %s/%s", workState, sandbox)
	}
	if err := lifecycle.Reclaim(ctx, f.target); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("delete attempts = %d, want 2", len(calls))
	}
	provider := &recordingRunnerProvider{sandboxID: f.target.ProviderSandboxID + "-recovered"}
	cfg := f.app.cfg
	cfg.CodeSession.SandboxAPIBaseURL = "http://sandbox-api.example.test"
	runUntilSandboxCreated(t, ctx, newManagedAgentRunner(t, f.app, provider, cfg), provider)
	if workState, _ := f.state(t); workState != "active" {
		t.Fatalf("workState = %s", workState)
	}
	recovered, err := getCodeSession(f.app, ctx, f.code.ExternalID)
	if err != nil || recovered.UUID != f.code.UUID {
		t.Fatalf("recovered session = %+v, %v", recovered, err)
	}
	if len(provider.launches) != 1 {
		t.Fatalf("launch count = %d", len(provider.launches))
	}
	for len(calls) > 0 {
		id := <-calls
		if id != f.target.ProviderSandboxID {
			t.Fatalf("deleted replacement: %s", id)
		}
	}
}

func TestSandboxReclamationDryRunAndLazyWake(t *testing.T) {
	f := newSandboxLifecycleFixture(t)
	ctx := context.Background()
	var calls atomic.Int32
	killer := newLifecycleProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	cfg := config.SandboxLifecycleConfig{Enabled: true, DryRun: true, IdleTimeout: 24 * time.Hour}
	lifecycle := environments.NewSandboxLifecycle(f.app.db, killer, cfg, nil)
	if err := lifecycle.Reclaim(ctx, f.target); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("dry run deleted sandbox")
	}
	cfg.DryRun = false
	lifecycle = environments.NewSandboxLifecycle(f.app.db, killer, cfg, nil)
	if err := lifecycle.Reclaim(ctx, f.target); err != nil {
		t.Fatal(err)
	}
	if workState, sandbox := f.state(t); workState != "active" || sandbox != "stopped" {
		t.Fatalf("state = %s/%s", workState, sandbox)
	}
	provider := &recordingRunnerProvider{sandboxID: f.target.ProviderSandboxID + "-unexpected"}
	if _, err := newManagedAgentRunner(t, f.app, provider, f.app.cfg).RunOnce(ctx, "idle-reclaim-check"); err != nil {
		t.Fatal(err)
	}
	if len(provider.launches) != 0 {
		t.Fatal("runner rebuilt an idle sandbox without new input")
	}
	if queued, err := f.app.db.ScheduleEnvironmentSandboxRecoveryForCodeSession(ctx, f.code.ExternalID, "", nil); err != nil || queued {
		t.Fatalf("recovery without input = %t, %v", queued, err)
	}
	if err := lifecycle.Reclaim(ctx, f.target); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("completed deletion repeated: %d calls", calls.Load())
	}
	sendSessionEvents(t, f.app, f.session.ExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"wake on new input"}]}]}`, defaultTestKey)
	if workState, _ := f.state(t); workState != "queued" {
		t.Fatalf("workState after new input = %s", workState)
	}
}

func TestSandboxIdleClockIgnoresHeartbeatsAndRepeatedIdle(t *testing.T) {
	f := newSandboxLifecycleFixture(t)
	var before, after time.Time
	read := func(target *time.Time) {
		t.Helper()
		if err := f.app.pool.QueryRow(context.Background(), `SELECT idle_since FROM code_sessions WHERE uuid=$1`, f.code.UUID).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	read(&before)
	assertCodeSessionWorkerHeartbeat(t, f.app, f.code.ExternalID, f.epoch)
	putCodeSessionWorkerState(t, f.app, f.code.ExternalID, `{"worker_epoch":`+f.epoch+`,"worker_status":"idle"}`)
	read(&after)
	if !before.Equal(after) {
		t.Fatalf("idle clock refreshed from %s to %s", before, after)
	}
}

func TestSandboxReclamationRejectsPublicInputBeforeForwarding(t *testing.T) {
	f := newSandboxLifecycleFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := f.app.db.AppendSessionEvents(ctx, f.code.WorkspaceUUID, f.session.ExternalID, []db.SessionEvent{{
		UUID: uuid.NewV4().String(), ExternalID: "sevt_" + uuid.NewV4().String(), EventType: "user.message",
		Payload:     json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"pending forwarding"}]}`),
		ProcessedAt: now, CreatedAt: now,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := f.app.db.BeginSandboxReclamation(ctx, f.target, now.Add(-24*time.Hour), true); err != nil || claimed {
		t.Fatalf("claimed after public input = %t, %v", claimed, err)
	}
}

func TestSandboxReclamationRecoveryRejectsIneligibleTargets(t *testing.T) {
	for _, tc := range []struct {
		name, state, reason string
		archived            bool
	}{
		{"running", "running", "", false},
		{"deletion in progress", "stopping", "idle_timeout", false},
		{"stopped for another reason", "stopped", "terminated", false},
		{"archived parent", "stopped", "idle_timeout", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSandboxLifecycleFixture(t)
			f.exec(t, `UPDATE environment_sandboxes SET state = $2, stop_reason = $3 WHERE uuid = $1`, f.target.SandboxUUID, tc.state, tc.reason)
			f.exec(t, `UPDATE code_session_inbound_events SET delivery_status = 'queued' WHERE code_session_uuid = $1`, f.code.UUID)
			if tc.archived {
				f.exec(t, `UPDATE sessions SET archived_at = NOW() WHERE uuid = $1`, f.session.UUID)
			}
			if queued, err := f.app.db.ScheduleEnvironmentSandboxRecoveryForCodeSession(context.Background(), f.code.ExternalID, "", nil); err != nil || queued {
				t.Fatalf("ineligible recovery = %t, %v", queued, err)
			}
		})
	}
}

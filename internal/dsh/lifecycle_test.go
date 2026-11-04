package dsh

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type lifecycleDatabase struct {
	state db.DSHWorkerState
	calls int
	err   error
}

func (d *lifecycleDatabase) RegisterDSHWorker(context.Context, db.DSHScope) (time.Time, error) {
	d.calls++
	return time.Now(), d.err
}
func (d *lifecycleDatabase) HeartbeatDSHWorker(context.Context, db.DSHScope) (time.Time, error) {
	d.calls++
	return time.Now(), d.err
}
func (d *lifecycleDatabase) GetDSHWorkerState(context.Context, db.DSHScope) (db.DSHWorkerState, error) {
	d.calls++
	return d.state, d.err
}
func (d *lifecycleDatabase) PatchDSHWorkerState(_ context.Context, _ db.DSHScope, patch db.DSHWorkerPatch) (db.DSHWorkerState, error) {
	d.calls++
	if patch.ModelSet {
		d.state.ModelControlSequence = &patch.ControlSequence
		d.state.Model = patch.Model
	}
	if patch.TraceTurnsSet {
		d.state.TraceTurns = patch.TraceTurns
	}
	return d.state, d.err
}

type testLifecycle struct{ claim Claim }

func (l testLifecycle) Claim(context.Context, string, time.Duration) (Claim, bool, error) {
	return l.claim, true, nil
}
func (l testLifecycle) Release(context.Context, string, string, int64, string) error { return nil }
func (l testLifecycle) Context(context.Context, db.DSHScope, string, string) (json.RawMessage, error) {
	return json.RawMessage(`{"models":{}}`), nil
}
func (l testLifecycle) Telemetry(context.Context, db.DSHScope) (bool, *TraceExport) {
	return false, nil
}

func controlRouter(database WorkerDatabase) http.Handler {
	router := chi.NewRouter()
	authenticate := func(_ *http.Request, _ string) (db.DSHScope, error) { return db.DSHScope{Epoch: 7}, nil }
	fleet := func(*http.Request) error { return nil }
	NewControlHandler(database, testLifecycle{}, authenticate, fleet, nil).RegisterRoutes(router)
	return router
}

func TestWorkerStateRejectsInvalidPatches(t *testing.T) {
	for _, body := range []string{`{"status":null}`, `{"status":"idle"}`, `{"status":"running"}`, `{"model":null}`, `{"model":"chosen"}`, `{"model":"chosen","control_input_id":"a","control_sequence":"0"}`, `{"model":"chosen","control_input_id":"a","control_sequence":"01"}`, `{"model":"chosen","control_input_id":"a","control_sequence":"18446744073709551616"}`, `{"status":"requires_action"}`, `{"model":""}`, `{"pending_action":{}}`, `{"trace_turns":{}}`, `{"trace_turns":[{"sessionId":"root","traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7"}]}`, `{"trace_turns":[{"sessionId":"root","traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7","startMs":null}]}`, `{"trace_turns":[{"sessionId":"root","traceId":"bad","spanId":"bad","startMs":0}]}`} {
		database := &lifecycleDatabase{state: db.DSHWorkerState{}}
		response := transcriptRequest(controlRouter(database), "PUT", "/sessions/cse_root/worker-state", body, "1")
		if response.Code != 400 || database.calls != 0 {
			t.Fatalf("body=%s status=%d calls=%d", body, response.Code, database.calls)
		}
	}
}

func TestWorkerStateSupportsTraceArrayAndExplicitClearing(t *testing.T) {
	database := &lifecycleDatabase{state: db.DSHWorkerState{}}
	router := controlRouter(database)
	response := transcriptRequest(router, "PUT", "/sessions/cse_root/worker-state", `{"model":"chosen","control_input_id":"one","control_sequence":"1","trace_turns":[{"sessionId":"root","traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7","startMs":0}]}`, "1")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if _, found := fields["status"]; found {
		t.Fatal("worker-state exposes status")
	}
	if response.Code != 200 || database.state.Model == nil || *database.state.Model != "chosen" || len(database.state.TraceTurns) == 0 {
		t.Fatalf("state=%+v response=%s", database.state, response.Body.String())
	}
	response = transcriptRequest(router, "PUT", "/sessions/cse_root/worker-state", `{"model":null,"control_input_id":"two","control_sequence":"2","trace_turns":null}`, "1")
	if response.Code != 200 || database.state.Model != nil || database.state.TraceTurns != nil {
		t.Fatalf("state=%+v response=%s", database.state, response.Body.String())
	}
}

func TestRegisterAndHeartbeatKeepEpochInTokenScope(t *testing.T) {
	database := &lifecycleDatabase{}
	router := controlRouter(database)
	response := transcriptRequest(router, "POST", "/sessions/cse_root/register", `{}`, "1")
	var registered registrationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || registered.Epoch != "7" || registered.Protocol != "1" || registered.Lease.TTLMillis != 20000 {
		t.Fatalf("response=%s", response.Body.String())
	}
	response = transcriptRequest(router, "POST", "/sessions/cse_root/heartbeat", `{"worker_epoch":7}`, "1")
	if response.Code != 400 {
		t.Fatalf("accepted body epoch: %s", response.Body.String())
	}
	response = transcriptRequest(router, "POST", "/sessions/cse_root/heartbeat", `{}`, "1")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
}

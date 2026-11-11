package dsh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type lifecycleDatabase struct {
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

type testLifecycle struct {
	claim           Claim
	registrationErr error
	renew           func(context.Context, db.DSHScope) error
}

func (l testLifecycle) Claim(context.Context, string, time.Duration) (Claim, bool, error) {
	return l.claim, true, nil
}
func (l testLifecycle) Release(context.Context, string, string, int64, string) error { return nil }
func (l testLifecycle) RegistrationContext(context.Context, db.DSHScope, string) (json.RawMessage, error) {
	return json.RawMessage(`{"models":{}}`), l.registrationErr
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

func TestRegisterAndHeartbeatKeepEpochInTokenScope(t *testing.T) {
	database := &lifecycleDatabase{}
	router := controlRouter(database)
	response := transcriptRequest(router, "POST", "/sessions/cse_root/register", `{}`, "1")
	var registered registrationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || registered.Epoch != "7" || registered.Protocol != "1" || registered.Lease.TTLMillis != 20000 || string(registered.Context) != `{"models":{}}` {
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

func TestContextRouteIsRemoved(t *testing.T) {
	database := &lifecycleDatabase{}
	response := transcriptRequest(controlRouter(database), "GET", "/sessions/cse_root/context", "", "1")
	if response.Code != 404 || database.calls != 0 {
		t.Fatalf("obsolete context route=%d calls=%d", response.Code, database.calls)
	}
}

func TestRegisterDoesNotEstablishLeaseWhenConfigurationFails(t *testing.T) {
	database := &lifecycleDatabase{}
	router := chi.NewRouter()
	NewControlHandler(database, testLifecycle{registrationErr: errors.New("snapshot unavailable")}, func(*http.Request, string) (db.DSHScope, error) { return db.DSHScope{Epoch: 7}, nil }, func(*http.Request) error { return nil }, nil).RegisterRoutes(router)
	response := transcriptRequest(router, "POST", "/sessions/cse_root/register", `{}`, "1")
	if response.Code != 500 || database.calls != 0 {
		t.Fatalf("registration=%d calls=%d", response.Code, database.calls)
	}
}

func TestWorkerStateRoutesAreUnavailable(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		database := &lifecycleDatabase{}
		response := transcriptRequest(controlRouter(database), method, "/sessions/cse_root/worker-state", `{}`, "1")
		if response.Code != 404 || database.calls != 0 {
			t.Fatalf("method=%s status=%d calls=%d", method, response.Code, database.calls)
		}
	}
}

func (l testLifecycle) RenewSandbox(ctx context.Context, scope db.DSHScope) error {
	if l.renew != nil {
		return l.renew(ctx, scope)
	}
	return nil
}

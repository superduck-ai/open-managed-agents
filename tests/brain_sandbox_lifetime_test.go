//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestBrainSandboxStartupFailureIsNotLeaseConflict(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("brain-sandbox-unavailable"))
	worker, _ := newPayloadIntegrationSession(t, app)
	claimed, err := registerTaskTestWorker(t, app, worker)
	if err != nil {
		t.Fatal(err)
	}
	token := codeSessionIngressToken(t, app, worker.ExternalID)
	response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/sandbox/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`), token, false)
	body := readAll(t, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "Sandbox is unavailable") {
		t.Fatalf("unavailable sandbox status=%d body=%s", response.StatusCode, body)
	}
	if err := app.db.ValidateCodeSessionIngressWorkerEpoch(t.Context(), worker.OrganizationUUID, worker.WorkspaceUUID, worker.ExternalID, claimed.CurrentWorkerEpoch); err != nil {
		t.Fatalf("sandbox startup failure invalidated the lease: %v", err)
	}
}

func TestBrainSandboxMCPResumesBeforeDispatch(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("brain-sandbox-lifetime"))
	worker, _ := newPayloadIntegrationSession(t, app)
	calls := 0
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if len(app.sandboxTimeouts.snapshotCalls()) < 2 {
			t.Error("dispatch preceded sandbox renewal")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"resumed"}]}}`))
	}))
	defer sandbox.Close()
	if _, err := registerTaskTestWorker(t, app, worker); err != nil {
		t.Fatal(err)
	}
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, slog.Default()).WithSandboxProvisioner(fixedSandboxProvisioner(sandbox.URL))
	handler := codesessions.NewHandler(app.cfg, service, app.sandboxTimeouts, slog.Default())
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	app.baseURL = gateway.URL
	token := codeSessionIngressToken(t, app, worker.ExternalID)
	endpoint := "/v2/ccr-sessions/" + worker.ExternalID + "/sandbox/mcp"
	app.sandboxTimeouts.setError(errors.New("resume unavailable"))
	request := func(body string) *http.Response {
		return doSessionRequest(t, app, http.MethodPost, endpoint, strings.NewReader(body), token, false)
	}
	response := request(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError || calls != 0 {
		t.Fatalf("failed resume status=%d dispatches=%d", response.StatusCode, calls)
	}
	app.sandboxTimeouts.setError(nil)
	response = request(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || calls != 1 {
		t.Fatalf("initialize status=%d dispatches=%d", response.StatusCode, calls)
	}
	response = request(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"bash","arguments":{"command":"printf resumed"}}}`)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("resumed dispatch status=%d calls=%d", response.StatusCode, calls)
	}
	renewals := app.sandboxTimeouts.snapshotCalls()
	if len(renewals) != 3 {
		t.Fatalf("renewals=%d want 3", len(renewals))
	}
	for _, call := range renewals {
		if call.sandboxID == "" || call.timeout != app.cfg.E2B.SandboxTimeout {
			t.Fatalf("invalid renewal: %+v", call)
		}
	}
}

type fixedSandboxProvisioner string

func (p fixedSandboxProvisioner) EnsureSessionSandbox(context.Context, db.CodeSession) (string, error) {
	return string(p), nil
}

//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"net/http"
	"strings"
	"testing"
)

func TestBrainSandboxStartupFailureIsNotLeaseConflict(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("brain-sandbox-unavailable"))
	worker, _ := newPayloadIntegrationSession(t, app)
	claimed, err := registerTaskTestWorker(t, app, worker)
	if err != nil {
		t.Fatal(err)
	}
	token := codeSessionIngressToken(t, app, worker.ExternalID)
	response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/threads/root/tool-calls",
		strings.NewReader(`{"call_id":"startup","tool":"bash","input":{"command":"true"}}`), token, false)
	body := readAll(t, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "Sandbox is unavailable") {
		t.Fatalf("unavailable sandbox status=%d body=%s", response.StatusCode, body)
	}
	if err := app.db.ValidateCodeSessionIngressWorkerEpoch(t.Context(), worker.OrganizationUUID, worker.WorkspaceUUID, worker.ExternalID, claimed.CurrentWorkerEpoch); err != nil {
		t.Fatalf("sandbox startup failure invalidated the lease: %v", err)
	}
}

func TestBrainToolSubmissionResumesBeforeEnqueue(t *testing.T) {
	app, worker, _, token := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"renewal"}`)
	endpoint := "/v2/ccr-sessions/" + worker.ExternalID + "/threads/root/tool-calls"
	app.sandboxTimeouts.setError(errors.New("resume unavailable"))
	request := func() *http.Response {
		return doSessionRequest(t, app, http.MethodPost, endpoint, strings.NewReader(`{"call_id":"resume","tool":"bash","input":{"command":"true"}}`), token, false)
	}
	response := request()
	response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("failed resume status=%d", response.StatusCode)
	}
	app.sandboxTimeouts.setError(nil)
	response = request()
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("resumed status=%d", response.StatusCode)
	}
	if len(app.sandboxTimeouts.snapshotCalls()) < 2 {
		t.Fatal("submission did not retry provider renewal")
	}
}

type fixedSandboxProvisioner struct {
	db *db.DB
}

func (p fixedSandboxProvisioner) EnsureSessionSandbox(ctx context.Context, code db.CodeSession) (db.EnvironmentSandbox, error) {
	sandbox, err := p.db.GetResumableEnvironmentSandboxForCodeSession(ctx, code.ExternalID)
	return sandbox, err
}

//go:build integration

package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionIngressSeparatesTokenIdentityFromWorkerAuthority(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("ingress-authority"))
	code, _ := newPayloadIntegrationSession(t, app)
	token := codeSessionIngressToken(t, app, code.ExternalID)
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil)
	epoch, err := app.db.RotateManagedAgentCodeSessionCredentials(t.Context(), db.Session{
		OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, ExternalID: code.SessionExternalID,
	}, code.ExternalID, "replacement-ingress-authority")
	if err != nil {
		t.Fatal(err)
	}
	workerPath := "/v1/code/sessions/" + code.ExternalID + "/worker"
	for _, operation := range []struct{ method, suffix string }{
		{http.MethodPost, "/register"},
		{http.MethodPost, "/heartbeat"},
		{http.MethodPut, ""},
		{http.MethodPost, "/events"},
		{http.MethodPost, "/events/delivery"},
		{http.MethodPost, "/diagnostics"},
		{http.MethodPost, "/internal-events"},
		{http.MethodPost, "/otlp/v1/traces"},
		{http.MethodGet, "/events/stream"},
	} {
		t.Run("failure stale owner "+operation.method+operation.suffix, func(t *testing.T) {
			body := fmt.Sprintf(`{"worker_epoch":%d}`, epoch)
			response := doSessionRequest(t, app, operation.method, workerPath+operation.suffix,
				strings.NewReader(body), token, false)
			defer response.Body.Close()
			if response.StatusCode != http.StatusConflict {
				t.Fatalf("status=%d want=409: %s", response.StatusCode, readAll(t, response.Body))
			}
		})
	}
	t.Run("failure foreign tenant read", func(t *testing.T) {
		foreign := codeSessionIngressTokenWithScope(t, app, code.ExternalID, "00000000-0000-4000-8000-000000000001", "")
		response := doSessionRequest(t, app, http.MethodGet, workerPath, nil, foreign, false)
		assertError(t, response, http.StatusUnauthorized, "authentication_error")
	})
	t.Run("success old token identity", func(t *testing.T) {
		claims, err := service.AuthenticateSessionIngress(t.Context(), token, code.ExternalID)
		if err != nil || claims.WorkerEpoch != code.CurrentWorkerEpoch {
			t.Fatalf("old token identity rejected: epoch=%d err=%v", claims.WorkerEpoch, err)
		}
	})
	for _, path := range []string{"/v2/sessions/" + code.ExternalID, workerPath, workerPath + "/internal-events"} {
		t.Run("success old token read "+path, func(t *testing.T) {
			response := doSessionRequest(t, app, http.MethodGet, path, nil, token, false)
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status=%d want=200: %s", response.StatusCode, readAll(t, response.Body))
			}
		})
	}
}

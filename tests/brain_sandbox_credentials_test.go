//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func assertBrainSandboxCredentialLifecycle(t *testing.T, app *testApp, service *codesessions.Service, code db.CodeSession, providers []*provisioningTestProvider) {
	t.Helper()
	var token string
	for _, provider := range providers {
		for _, launch := range provider.launches {
			var payload struct {
				Auth []struct {
					Token string `json:"token"`
				} `json:"auth"`
			}
			if err := json.Unmarshal(launch.stdin, &payload); err != nil || len(payload.Auth) != 1 {
				t.Fatal("sandbox auth payload missing")
			}
			token = payload.Auth[0].Token
		}
	}
	claims, err := app.credentials.VerifySandbox(token)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []struct{ org, workspace, session, sandbox string }{
		{"00000000-0000-0000-0000-000000000000", claims.WorkspaceUUID, claims.SessionID, claims.SandboxUUID},
		{claims.OrganizationUUID, "00000000-0000-0000-0000-000000000000", claims.SessionID, claims.SandboxUUID},
		{claims.OrganizationUUID, claims.WorkspaceUUID, "cse_other", claims.SandboxUUID},
		{claims.OrganizationUUID, claims.WorkspaceUUID, claims.SessionID, "00000000-0000-0000-0000-000000000000"},
	} {
		if err := app.db.ValidateCodeSessionSandbox(t.Context(), scope.org, scope.workspace, scope.session, scope.sandbox); err == nil {
			t.Fatal("accepted a sandbox credential outside its scope")
		}
	}
	if _, err := service.AuthenticateSessionIngress(t.Context(), token, code.ExternalID); err == nil {
		t.Fatal("sandbox credential authenticated as a worker")
	}
	first, found, err := service.ClaimBrainWorker(t.Context(), "proxy-owner-1", 0)
	if err != nil || !found || first.CodeSessionID != code.ExternalID {
		t.Fatalf("first owner claim: found=%t err=%v", found, err)
	}
	if err := service.ReleaseBrainClaim(t.Context(), code.ExternalID, "proxy-owner-1", first.WorkerEpoch, codesessions.BrainClaimShutdown); err != nil {
		t.Fatal(err)
	}
	second, found, err := service.ClaimBrainWorker(t.Context(), "proxy-owner-2", 0)
	if err != nil || !found || second.WorkerEpoch <= first.WorkerEpoch {
		t.Fatalf("replacement owner claim: found=%t err=%v", found, err)
	}
	if _, err := service.AuthenticateSessionIngress(t.Context(), first.IngressToken, code.ExternalID); err != nil {
		t.Fatalf("old worker token identity rejected after owner replacement: %v", err)
	}
	assertSandboxProxyAuthentication(t, app, first.IngressToken, http.StatusConflict)
	assertSandboxProxyAuthentication(t, app, token, http.StatusUnsupportedMediaType)
	response := requestCommitSignature(t, app, code.ExternalID, token, `{"contents":"sandbox signature after owner replacement"}`)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("sandbox commit signing status=%d", response.StatusCode)
	}
	sandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(t.Context(), code.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.db.UpdateEnvironmentSandboxState(t.Context(), sandbox.WorkspaceUUID, sandbox.ExternalID, "stopping", sandbox.ProviderSandboxID, nil, nil); err != nil {
		t.Fatal(err)
	}
	assertSandboxProxyAuthentication(t, app, token, http.StatusUnauthorized)
}

func assertSandboxProxyAuthentication(t *testing.T, app *testApp, token string, status int) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/code/upstreamproxy/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("proxy authentication status=%d want=%d", response.StatusCode, status)
	}
}

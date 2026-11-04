package codesessions

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestDSHRoutesRejectLegacyTokensBeforeStorageAccess(t *testing.T) {
	now := time.Now()
	credentials := newTestSessionCredentials(t, &now)
	identity := testSessionCredentialIdentity()
	identity.WorkerEpoch = 7
	legacy, err := credentials.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithCredentials(nil, credentials, nil)
	handler := NewHandler(config.Config{}, service, nil, nil)
	request := httptest.NewRequest("GET", "/v1/dsr/sessions/"+identity.SessionID+"/transcripts", nil)
	request.Header.Set("Authorization", "Bearer "+legacy)
	if _, err := handler.authenticateDSH(request, identity.SessionID); err == nil {
		t.Fatal("legacy token admitted to DSR")
	}
}

func TestDSHProtocolIsSignedAndPreserved(t *testing.T) {
	now := time.Now()
	credentials := newTestSessionCredentials(t, &now)
	identity := testSessionCredentialIdentity()
	identity.WorkerEpoch = 7
	identity.Protocol = "dsr1"
	token, err := credentials.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := credentials.Verify(token)
	if err != nil || claims.Protocol != "dsr1" || claims.WorkerEpoch != 7 {
		t.Fatalf("claims=%+v,error=%v", claims, err)
	}
	if _, err := credentials.Verify(tamperJWT(t, token)); err == nil {
		t.Fatal("tampered protocol accepted")
	}
	service := NewServiceWithCredentials(nil, credentials, nil)
	if _, err := service.AuthenticateSessionIngress(context.Background(), token, "cse_other"); err == nil {
		t.Fatal("cross-session token accepted")
	}
}

func TestWorkerProtocolRouteIsolation(t *testing.T) {
	cases := []struct {
		protocol, path string
		allowed        bool
	}{
		{"dsr1", "/v1/code/sessions/cse_root/worker/events", false},
		{"dsr1", "/v2/ccr-sessions/cse_root/mcp", false},
		{"", "/v1/dsr/sessions/cse_root/transcripts", false},
		{"dsr1", "/v1/dsr/sessions/cse_root/transcripts", true},
		{"", "/v1/code/sessions/cse_root/worker/events", true},
		{"unknown", "/v1/code/sessions/cse_root/worker/events", false},
	}
	for _, test := range cases {
		if got := workerProtocolMatchesPath(SessionCredentialClaims{Protocol: test.protocol}, test.path); got != test.allowed {
			t.Fatalf("protocol=%q path=%q allowed=%v", test.protocol, test.path, got)
		}
	}
}

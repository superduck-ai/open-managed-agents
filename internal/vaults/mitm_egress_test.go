package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPrepareSessionVaultMountRequiresMITMForMCPCredentials(t *testing.T) {
	t.Parallel()

	cred := db.VaultCredential{
		AuthType: "static_bearer",
		Auth:     json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://mcp.example.com/mcp"}`),
	}
	_, err := PrepareEnvCredentialMount(false, []db.VaultCredential{cred})
	if !errors.Is(err, ErrMITMRequiredForMCPCredentials) {
		t.Fatalf("error = %v, want ErrMITMRequiredForMCPCredentials", err)
	}

	_, err = PrepareEnvCredentialMount(true, []db.VaultCredential{cred})
	if err != nil {
		t.Fatalf("MITM on should allow MCP credentials: %v", err)
	}
}

type recordingTransport struct {
	lastAuth string
	calls    int
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	t.lastAuth = req.Header.Get("Authorization")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestMITMEgressPrepareInjectsBearerAndStripsClientAuthorization(t *testing.T) {
	t.Parallel()

	svc := newTestSecretsService(t)
	cred := sealedStaticBearerCredential(t, svc, "https://mcp.example.com/mcp", "vault-token", "cred_a")
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{cred},
	}
	injector := newTestInjector(t, svc, store, nil, time.Time{})
	egress := NewMITMEgress(nil, injector)
	recorder := &recordingTransport{}

	req := newHTTPRequest(t, http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer ")

	tripper, err := egress.Prepare(
		context.Background(),
		EgressSession{
			CodeSessionExternalID: "cse_test",
			OrganizationUUID:      testInjectOrgUUID,
			WorkspaceUUID:         testInjectWsUUID,
		},
		"mcp.example.com:443",
		req,
		recorder,
	)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	outReq := newHTTPRequest(t, http.MethodPost, "https://mcp.example.com/mcp", strings.NewReader(`{}`))
	outReq.Header.Set("Authorization", "Bearer ")
	resp, err := tripper.RoundTrip(outReq)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if recorder.lastAuth != "Bearer vault-token" {
		t.Fatalf("Authorization = %q, want vault bearer", recorder.lastAuth)
	}
}

func TestMITMEgressPrepareEnvThenInjectOrder(t *testing.T) {
	t.Parallel()

	svc := newTestSecretsService(t)
	mcpCred := sealedStaticBearerCredential(t, svc, "https://mcp.example.com/mcp", "vault-token", "cred_mcp")
	envCred := sealedEnvCredentialWithLocation(t, svc, "API_KEY", "oma_ph_key", "mcp.example.com", "real-secret", true, false)
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{mcpCred, envCred},
	}
	injector := newTestInjector(t, svc, store, nil, time.Time{})
	substitutor := newEgressSubstitutor(store, svc, nil)
	egress := NewMITMEgress(substitutor, injector)
	recorder := &recordingTransport{}

	req := newHTTPRequest(t, http.MethodPost, "/mcp", nil)
	req.Header.Set("X-Api-Key", "oma_ph_key")
	req.Header.Set("Authorization", "Bearer broken")

	tripper, err := egress.Prepare(
		context.Background(),
		EgressSession{
			CodeSessionExternalID: "cse_test",
			OrganizationUUID:      testInjectOrgUUID,
			WorkspaceUUID:         testInjectWsUUID,
		},
		"mcp.example.com:443",
		req,
		recorder,
	)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if req.Header.Get("X-Api-Key") != "real-secret" {
		t.Fatalf("env substitute before inject: X-Api-Key = %q", req.Header.Get("X-Api-Key"))
	}

	outReq := newHTTPRequest(t, http.MethodPost, "https://mcp.example.com/mcp", nil)
	outReq.Header.Set("X-Api-Key", req.Header.Get("X-Api-Key"))
	outReq.Header.Set("Authorization", "Bearer broken")
	resp, err := tripper.RoundTrip(outReq)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if recorder.lastAuth != "Bearer vault-token" {
		t.Fatalf("Authorization = %q", recorder.lastAuth)
	}
}

func newHTTPRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

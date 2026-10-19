package vaults

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMCPTransportNoEndpointMatchIsAnonymousAndGenericPolicyIsUnchanged(t *testing.T) {
	for _, test := range []struct {
		name, credentialURL, targetURL string
		mcp, missingSecret, wantError  bool
		wantStatus                     int
		wantAuth                       string
	}{
		{name: "MCP different query values stay anonymous", credentialURL: "https://mcp.example.com/mcp?tenant=A", targetURL: "https://mcp.example.com/mcp?tenant=B", mcp: true, wantStatus: 401},
		{name: "MCP missing query is not same endpoint", credentialURL: "https://mcp.example.com/mcp?tenant=A", mcp: true, wantStatus: 401},
		{name: "MCP extra query is not same endpoint", credentialURL: testInjectMCPURL, targetURL: "https://mcp.example.com/mcp?tenant=B", mcp: true, wantStatus: 401},
		{name: "MCP exact query keeps host port and slash normalization", credentialURL: "https://MCP.EXAMPLE.COM:443/mcp/?tenant=A", targetURL: "https://mcp.example.com/mcp?tenant=A", mcp: true, wantStatus: 200, wantAuth: "Bearer local-token"},
		{name: "generic query retains prefix behavior", credentialURL: "https://mcp.example.com/mcp?tenant=A", targetURL: "https://mcp.example.com/mcp?tenant=B", wantStatus: 200, wantAuth: "Bearer local-token"},
		{name: "MCP sibling path", credentialURL: "https://mcp.example.com/other", mcp: true, wantStatus: 401},
		{name: "MCP broader prefix", credentialURL: "https://mcp.example.com/", mcp: true, wantStatus: 401},
		{name: "MCP narrower path", credentialURL: "https://mcp.example.com/mcp/other", mcp: true, wantStatus: 401},
		{name: "MCP normalized exact", credentialURL: "https://MCP.EXAMPLE.COM:443/mcp/", mcp: true, wantStatus: 200, wantAuth: "Bearer local-token"},
		{name: "MCP matched unreadable secret", credentialURL: testInjectMCPURL, mcp: true, missingSecret: true, wantError: true},
		{name: "generic sibling stays fail closed", credentialURL: "https://mcp.example.com/other", wantError: true},
		{name: "generic prefix still injects", credentialURL: "https://mcp.example.com/", wantStatus: 200, wantAuth: "Bearer local-token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := newTestSecretsService(t)
			credential := sealedStaticBearerCredential(t, svc, test.credentialURL, "local-token", "cred_one")
			if test.missingSecret {
				credential.SecretEnvelope = nil
			}
			store := &fakeCredentialStore{vaultIDs: []string{"vlt_one"}, credentials: []db.VaultCredential{credential}}
			injector := newTestInjector(t, svc, store, nil, time.Time{})
			calls, received := 0, ""
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				received = r.Header.Get("Authorization")
				if received == "Bearer local-token" {
					w.WriteHeader(200)
				} else {
					w.WriteHeader(401)
				}
			}))
			defer upstream.Close()
			targetURL := test.targetURL
			if targetURL == "" {
				targetURL = testInjectMCPURL
			}
			target, err := url.Parse(targetURL)
			if err != nil {
				t.Fatal(err)
			}
			wrap := injector.WrapTransport
			if test.mcp {
				wrap = injector.WrapMCPTransport
			}
			transport := wrap(context.Background(), "cse_test", testInjectOrgUUID, testInjectWsUUID, target, upstream.Client().Transport)
			req, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer unrelated-client")
			resp, err := transport.RoundTrip(req)
			if test.wantError {
				if !errors.Is(err, ErrInjectionRejected) || calls != 0 {
					t.Fatalf("want local rejection without upstream call; err=%v calls=%d", err, calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != test.wantStatus || calls != 1 || received != test.wantAuth {
				t.Fatalf("status=%d calls=%d auth=%q", resp.StatusCode, calls, received)
			}
		})
	}
}

func TestMCPTransportNeverTriesBroaderCredentialBeforeExactEndpoint(t *testing.T) {
	svc := newTestSecretsService(t)
	broader := sealedStaticBearerCredential(t, svc, "https://mcp.example.com/", "local-wrong", "cred_broad")
	exact := sealedStaticBearerCredential(t, svc, testInjectMCPURL, "local-right", "cred_exact")
	store := &fakeCredentialStore{vaultIDs: []string{"vlt_one"}, credentials: []db.VaultCredential{broader, exact}}
	injector := newTestInjector(t, svc, store, nil, time.Time{})
	upstream, probe := newMCPAuthProbeUpstream(t, "local-right")
	target, err := url.Parse(testInjectMCPURL)
	if err != nil {
		t.Fatal(err)
	}
	transport := injector.WrapMCPTransport(context.Background(), "cse_test", testInjectOrgUUID, testInjectWsUUID, target, upstream.Client().Transport)
	req, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || probe.calls != 1 || probe.last != "Bearer local-right" {
		t.Fatalf("status=%d calls=%d", resp.StatusCode, probe.calls)
	}
}

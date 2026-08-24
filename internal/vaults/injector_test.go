package vaults

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestOpenStaticBearerTokenMissingEnvelope(t *testing.T) {
	svc := newTestSecretsService(t)
	injector := newTestInjector(t, svc, nil, nil, time.Time{})
	if _, err := injector.openStaticBearerToken(context.Background(), &db.VaultCredential{ExternalID: "cred_missing"}); err == nil {
		t.Fatal("expected error for missing envelope")
	}
}

func TestOpenStaticBearerToken(t *testing.T) {
	svc := newTestSecretsService(t)
	injector := newTestInjector(t, svc, nil, nil, time.Time{})
	credential := &db.VaultCredential{
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
		VaultExternalID:  "vlt_test",
		ExternalID:       "cred_test",
	}
	payload, err := json.Marshal(map[string]any{"type": "static_bearer", "token": "super-secret-token"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	credential.SecretPayload = payload
	if err := SealCredentialSecret(context.Background(), svc, credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	credential.SecretPayload = json.RawMessage(`{"caller":"owned"}`)

	token, err := injector.openStaticBearerToken(context.Background(), credential)
	if err != nil {
		t.Fatalf("openStaticBearerToken: %v", err)
	}
	if token != "super-secret-token" {
		t.Fatalf("token = %q", token)
	}
	if string(credential.SecretPayload) != `{"caller":"owned"}` {
		t.Fatalf("caller payload was modified: %s", credential.SecretPayload)
	}
}

const (
	testInjectOrgUUID = "00000000-0000-0000-0000-000000000001"
	testInjectWsUUID  = "00000000-0000-0000-0000-000000000002"
	testInjectMCPURL  = "https://mcp.example.com/mcp"
)

type mcpAuthProbe struct {
	calls int
	last  string
	seen  []string
}

// newMCPAuthProbeUpstream returns 200 only for Bearer okToken; all other auths get 401.
func newMCPAuthProbeUpstream(t *testing.T, okToken string) (*httptest.Server, *mcpAuthProbe) {
	t.Helper()
	probe := &mcpAuthProbe{}
	ok := "Bearer " + okToken
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe.calls++
		auth := r.Header.Get("Authorization")
		probe.last = auth
		probe.seen = append(probe.seen, auth)
		if auth == ok {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok")
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv, probe
}

func newOAuthAccessTokenServer(t *testing.T, accessToken string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": accessToken,
			"expires_in":   3600,
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newOAuthInvalidGrantServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func wrapTransportRoundTrip(t *testing.T, injector *Injector, upstream *httptest.Server) *http.Response {
	t.Helper()
	return wrapTransportRoundTripWithin(t, injector, upstream, 0)
}

func wrapTransportRoundTripWithin(
	t *testing.T,
	injector *Injector,
	upstream *httptest.Server,
	timeout time.Duration,
) *http.Response {
	t.Helper()
	target, err := url.Parse(testInjectMCPURL)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	transport := injector.WrapTransport(
		context.Background(),
		"cse_test",
		testInjectOrgUUID,
		testInjectWsUUID,
		target,
		upstream.Client().Transport,
	)
	req, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	var resp *http.Response
	var tripErr error
	if timeout <= 0 {
		resp, tripErr = transport.RoundTrip(req)
	} else {
		done := make(chan struct{})
		go func() {
			defer close(done)
			resp, tripErr = transport.RoundTrip(req)
		}()
		select {
		case <-done:
		case <-time.After(timeout):
			t.Fatal("RoundTrip hung; excluded map likely keyed by empty ExternalID")
		}
	}
	if tripErr != nil {
		t.Fatalf("RoundTrip: %v", tripErr)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestWrapTransportLoadsCredentialsOnceAcrossUnauthorizedWalk(t *testing.T) {
	svc := newTestSecretsService(t)
	first := sealedStaticBearerCredential(t, svc, testInjectMCPURL, "tok-a", "cred_a")
	second := sealedStaticBearerCredential(t, svc, testInjectMCPURL, "tok-b", "cred_b")
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_a", "vlt_b"},
		credentials: []db.VaultCredential{first, second},
	}
	upstream, probe := newMCPAuthProbeUpstream(t, "tok-b")
	injector := newTestInjector(t, svc, store, nil, time.Time{})

	resp := wrapTransportRoundTrip(t, injector, upstream)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, lastAuth=%q", resp.StatusCode, probe.last)
	}
	if probe.last != "Bearer tok-b" {
		t.Fatalf("lastAuth = %q, want Bearer tok-b", probe.last)
	}
	if store.vaultIDCalls != 1 || store.credentialCalls != 1 {
		t.Fatalf("loader calls vaultIDs=%d credentials=%d, want 1 each", store.vaultIDCalls, store.credentialCalls)
	}
	if probe.calls != 2 {
		t.Fatalf("upstream calls = %d, want 2", probe.calls)
	}
}

func TestWrapTransportMCPOAuthUnauthorizedRefreshesAndRetries(t *testing.T) {
	svc := newTestSecretsService(t)
	tokenServer, tokenCalls := newOAuthAccessTokenServer(t, "fresh-access")
	// expires_at is in the future so the first inject reuses the stored token;
	// the upstream 401 then triggers a forced refresh.
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	credential := sealedMCPOAuthCredential(t, svc, tokenServer.URL, "stale-access", "refresh-token", strPtr("2026-08-10T18:00:00Z"))
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_o"},
		credentials: []db.VaultCredential{credential},
		getResults:  []db.VaultCredential{credential},
	}
	upstream, probe := newMCPAuthProbeUpstream(t, "fresh-access")
	injector := newTestInjector(t, svc, store, tokenServer.Client(), now)

	resp := wrapTransportRoundTrip(t, injector, upstream)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, lastAuth=%q", resp.StatusCode, probe.last)
	}
	if probe.last != "Bearer fresh-access" {
		t.Fatalf("lastAuth = %q, want Bearer fresh-access", probe.last)
	}
	if tokenCalls.Load() != 1 || probe.calls != 2 {
		t.Fatalf("tokenCalls=%d upstreamCalls=%d", tokenCalls.Load(), probe.calls)
	}
	if store.updateCalls != 1 {
		t.Fatalf("updateCalls=%d", store.updateCalls)
	}
	if store.vaultIDCalls != 1 || store.credentialCalls != 1 {
		t.Fatalf("loader calls vaultIDs=%d credentials=%d", store.vaultIDCalls, store.credentialCalls)
	}
}

func TestWrapTransportExcludesByPlanCredIDWhenUpdateReturnsEmptyRow(t *testing.T) {
	// fake UpdateVaultCredential returns a zero-value row (empty ExternalID).
	// Walk state must key off planCredID or the same mcp_oauth entry is
	// re-selected forever after a post-refresh 401.
	svc := newTestSecretsService(t)
	tokenServer, _ := newOAuthAccessTokenServer(t, "fresh-access")
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	oauth := sealedMCPOAuthCredential(t, svc, tokenServer.URL, "stale-access", "refresh-token", strPtr("2026-08-10T18:00:00Z"))
	fallback := sealedStaticBearerCredential(t, svc, testInjectMCPURL, "tok-b", "cred_b")
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_o", "vlt_b"},
		credentials: []db.VaultCredential{oauth, fallback},
		getResults:  []db.VaultCredential{oauth},
	}
	upstream, probe := newMCPAuthProbeUpstream(t, "tok-b")
	injector := newTestInjector(t, svc, store, tokenServer.Client(), now)

	resp := wrapTransportRoundTripWithin(t, injector, upstream, 3*time.Second)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, seen=%v", resp.StatusCode, probe.seen)
	}
	if len(probe.seen) < 2 || probe.seen[len(probe.seen)-1] != "Bearer tok-b" {
		t.Fatalf("seen = %v, want walk to tok-b", probe.seen)
	}
}

type fakeCredentialStore struct {
	updateErr       error
	get             db.VaultCredential
	getResults      []db.VaultCredential
	vaultIDs        []string
	credentials     []db.VaultCredential
	updateCalls     int
	getCalls        int
	vaultIDCalls    int
	credentialCalls int
}

func (f *fakeCredentialStore) UpdateVaultCredential(
	_ context.Context,
	_, _, _ string,
	_ db.VaultCredential,
) (db.VaultCredential, error) {
	f.updateCalls++
	if f.updateErr != nil {
		return db.VaultCredential{}, f.updateErr
	}
	return db.VaultCredential{}, nil
}

func (f *fakeCredentialStore) GetVaultCredential(
	_ context.Context,
	_, _, _ string,
) (db.VaultCredential, error) {
	f.getCalls++
	if len(f.getResults) > 0 {
		row := f.getResults[0]
		f.getResults = f.getResults[1:]
		return row, nil
	}
	return f.get, nil
}

func (f *fakeCredentialStore) GetCodeSessionVaultIDs(context.Context, string, string, string) ([]string, error) {
	f.vaultIDCalls++
	return append([]string(nil), f.vaultIDs...), nil
}

func (f *fakeCredentialStore) ListActiveVaultCredentialsForVaultIDs(context.Context, string, []string) ([]db.VaultCredential, error) {
	f.credentialCalls++
	out := make([]db.VaultCredential, len(f.credentials))
	copy(out, f.credentials)
	return out, nil
}

func sealedStaticBearerCredential(t *testing.T, svc *secrets.Service, serverURL, token, id string) db.VaultCredential {
	t.Helper()
	auth, err := json.Marshal(map[string]any{"type": "static_bearer", "mcp_server_url": serverURL})
	if err != nil {
		t.Fatalf("marshal auth: %v", err)
	}
	secret, err := json.Marshal(map[string]any{"type": "static_bearer", "token": token})
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	credential := db.VaultCredential{
		OrganizationUUID: testInjectOrgUUID,
		WorkspaceUUID:    testInjectWsUUID,
		VaultExternalID:  "vlt_" + id,
		ExternalID:       id,
		AuthType:         "static_bearer",
		Auth:             auth,
		SecretPayload:    secret,
	}
	if err := SealCredentialSecret(context.Background(), svc, &credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	return credential
}

func newTestSecretsService(t *testing.T) *secrets.Service {
	t.Helper()
	kek, err := secrets.GenerateKEK()
	if err != nil {
		t.Fatalf("generate KEK: %v", err)
	}
	svc, err := secrets.NewLocalService(context.Background(), kek)
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	return svc
}

// newTestInjector builds via NewInjector then applies test overrides so
// logger/construction match production.
func newTestInjector(t *testing.T, svc *secrets.Service, store credentialStore, httpClient *http.Client, now time.Time) *Injector {
	t.Helper()
	injector := NewInjector(nil, svc, nil)
	injector.store = store
	injector.httpClient = httpClient
	if !now.IsZero() {
		injector.now = func() time.Time { return now }
	}
	return injector
}

func TestReadWithinLimitRejectsTruncation(t *testing.T) {
	const max = 8
	ok, err := readWithinLimit(strings.NewReader("12345678"), max)
	if err != nil {
		t.Fatalf("exact limit: %v", err)
	}
	if string(ok) != "12345678" {
		t.Fatalf("got %q", ok)
	}
	_, err = readWithinLimit(strings.NewReader("123456789"), max)
	if !errors.Is(err, errSnapshotRequestBodyTooLarge) {
		t.Fatalf("over limit err = %v", err)
	}
}

func TestSnapshotRequestBodyRejectsOversizedContentLength(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://mcp.example.com/mcp", bytes.NewReader([]byte(`{"tiny":true}`)))
	req.ContentLength = maxSnapshotRequestBodyBytes + 1
	_, err := snapshotRequestBody(req)
	if !errors.Is(err, errSnapshotRequestBodyTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestWrapTransportClosesOriginalRequestBody(t *testing.T) {
	svc := newTestSecretsService(t)
	store := &fakeCredentialStore{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	injector := newTestInjector(t, svc, store, nil, time.Time{})
	target, err := url.Parse(testInjectMCPURL)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	transport := injector.WrapTransport(
		context.Background(),
		"cse_test",
		testInjectOrgUUID,
		testInjectWsUUID,
		target,
		upstream.Client().Transport,
	)

	t.Run("failure oversized ContentLength still closes body", func(t *testing.T) {
		body := &closeTrackingBody{Reader: bytes.NewReader([]byte(`{"tiny":true}`))}
		req, err := http.NewRequest(http.MethodPost, upstream.URL, body)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.ContentLength = maxSnapshotRequestBodyBytes + 1
		_, err = transport.RoundTrip(req)
		if !errors.Is(err, errSnapshotRequestBodyTooLarge) {
			t.Fatalf("err = %v, want oversized", err)
		}
		if !body.closed.Load() {
			t.Fatal("expected original request body to be closed on snapshot failure")
		}
	})

	t.Run("success closes body", func(t *testing.T) {
		body := &closeTrackingBody{Reader: bytes.NewReader([]byte(`{"ok":true}`))}
		req, err := http.NewRequest(http.MethodPost, upstream.URL, body)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		if !body.closed.Load() {
			t.Fatal("expected original request body to be closed")
		}
	})
}

type closeTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *closeTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestSnapshotRequestBodyBuffersSmallBody(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"tools/list"}`)
	req := httptest.NewRequest(http.MethodPost, "https://mcp.example.com/mcp", bytes.NewReader(payload))
	got, err := snapshotRequestBody(req)
	if err != nil {
		t.Fatalf("snapshotRequestBody: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q", got)
	}
	reread, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if !bytes.Equal(reread, payload) {
		t.Fatalf("restored body %q", reread)
	}
}

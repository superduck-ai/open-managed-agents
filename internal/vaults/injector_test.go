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

func TestWrapTransportLoadsCredentialsOnceAcrossUnauthorizedWalk(t *testing.T) {
	svc := newTestSecretsService(t)
	first := sealedStaticBearerCredential(t, svc, "https://mcp.example.com/mcp", "tok-a", "cred_a")
	second := sealedStaticBearerCredential(t, svc, "https://mcp.example.com/mcp", "tok-b", "cred_b")
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_a", "vlt_b"},
		credentials: []db.VaultCredential{first, second},
	}
	upstreamCalls := 0
	var lastAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		auth := r.Header.Get("Authorization")
		lastAuth = auth
		if auth == "Bearer tok-a" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if auth == "Bearer tok-b" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok")
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	injector := newTestInjector(t, svc, store, nil, time.Time{})
	target, err := url.Parse("https://mcp.example.com/mcp")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	transport := injector.WrapTransport(
		context.Background(),
		"cse_test",
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		target,
		upstream.Client().Transport,
	)
	req, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, lastAuth=%q", resp.StatusCode, lastAuth)
	}
	if lastAuth != "Bearer tok-b" {
		t.Fatalf("lastAuth = %q, want Bearer tok-b", lastAuth)
	}
	if store.vaultIDCalls != 1 || store.credentialCalls != 1 {
		t.Fatalf("loader calls vaultIDs=%d credentials=%d, want 1 each", store.vaultIDCalls, store.credentialCalls)
	}
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
	}
}

func TestWrapTransportMCPOAuthUnauthorizedRefreshesAndRetries(t *testing.T) {
	svc := newTestSecretsService(t)
	tokenCalls := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-access", "expires_in": 3600})
	}))
	defer tokenServer.Close()

	// expires_at is in the future so the first inject reuses the stored token;
	// the upstream 401 then triggers a forced refresh.
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	credential := sealedMCPOAuthCredential(t, svc, tokenServer.URL, "stale-access", "refresh-token", strPtr("2026-08-10T18:00:00Z"))
	store := &fakeCredentialStore{getResults: []db.VaultCredential{credential}}
	store.vaultIDs = []string{"vlt_o"}
	store.credentials = []db.VaultCredential{credential}

	upstreamCalls := 0
	var lastAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		auth := r.Header.Get("Authorization")
		lastAuth = auth
		if auth == "Bearer stale-access" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if auth == "Bearer fresh-access" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok")
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	injector := newTestInjector(t, svc, store, tokenServer.Client(), now)
	target, err := url.Parse("https://mcp.example.com/mcp")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	transport := injector.WrapTransport(
		context.Background(),
		"cse_test",
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		target,
		upstream.Client().Transport,
	)
	req, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, lastAuth=%q", resp.StatusCode, lastAuth)
	}
	if lastAuth != "Bearer fresh-access" {
		t.Fatalf("lastAuth = %q, want Bearer fresh-access", lastAuth)
	}
	if tokenCalls != 1 || upstreamCalls != 2 {
		t.Fatalf("tokenCalls=%d upstreamCalls=%d", tokenCalls, upstreamCalls)
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
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-access", "expires_in": 3600})
	}))
	defer tokenServer.Close()

	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	oauth := sealedMCPOAuthCredential(t, svc, tokenServer.URL, "stale-access", "refresh-token", strPtr("2026-08-10T18:00:00Z"))
	fallback := sealedStaticBearerCredential(t, svc, "https://mcp.example.com/mcp", "tok-b", "cred_b")
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_o", "vlt_b"},
		credentials: []db.VaultCredential{oauth, fallback},
		getResults:  []db.VaultCredential{oauth},
	}

	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		seen = append(seen, auth)
		if auth == "Bearer tok-b" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok")
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	injector := newTestInjector(t, svc, store, tokenServer.Client(), now)
	target, err := url.Parse("https://mcp.example.com/mcp")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	transport := injector.WrapTransport(
		context.Background(),
		"cse_test",
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		target,
		upstream.Client().Transport,
	)
	req, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	done := make(chan struct{})
	var resp *http.Response
	var tripErr error
	go func() {
		defer close(done)
		resp, tripErr = transport.RoundTrip(req)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RoundTrip hung; excluded map likely keyed by empty ExternalID")
	}
	if tripErr != nil {
		t.Fatalf("RoundTrip: %v", tripErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, seen=%v", resp.StatusCode, seen)
	}
	if len(seen) < 2 || seen[len(seen)-1] != "Bearer tok-b" {
		t.Fatalf("seen = %v, want walk to tok-b", seen)
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
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
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

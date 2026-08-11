package vaults

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestDecodeMCPOAuthCredentialAuthErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "not mcp oauth", raw: `{"type":"static_bearer","mcp_server_url":"https://mcp.example.com/mcp"}`},
		{name: "missing mcp server url", raw: `{"type":"mcp_oauth"}`},
		{name: "invalid json", raw: `{"type":`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeMCPOAuthCredentialAuth([]byte(test.raw)); err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func TestAccessTokenExpired(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	t.Run("failure missing expires_at is not expired", func(t *testing.T) {
		expired, err := accessTokenExpired(nil, now)
		if err != nil || expired {
			t.Fatalf("expired=%v err=%v, want false nil", expired, err)
		}
	})
	t.Run("failure empty expires_at is not expired", func(t *testing.T) {
		empty := ""
		expired, err := accessTokenExpired(&empty, now)
		if err != nil || expired {
			t.Fatalf("expired=%v err=%v, want false nil", expired, err)
		}
	})
	t.Run("failure invalid expires_at", func(t *testing.T) {
		invalid := "not-a-time"
		if _, err := accessTokenExpired(&invalid, now); err == nil {
			t.Fatal("expected parse error")
		}
	})
	t.Run("success past expires_at", func(t *testing.T) {
		past := "2026-08-10T11:59:59Z"
		expired, err := accessTokenExpired(&past, now)
		if err != nil || !expired {
			t.Fatalf("expired=%v err=%v, want true nil", expired, err)
		}
	})
	t.Run("success future expires_at", func(t *testing.T) {
		future := "2026-08-10T12:00:01Z"
		expired, err := accessTokenExpired(&future, now)
		if err != nil || expired {
			t.Fatalf("expired=%v err=%v, want false nil", expired, err)
		}
	})
}

func TestHasMCPOAuthRefreshMaterial(t *testing.T) {
	t.Run("failure missing refresh blocks", func(t *testing.T) {
		auth := &mcpOAuthCredentialAuth{Type: credentialAuthTypeMCPOAuth, MCPServerURL: "https://mcp.example.com/mcp"}
		secret := mcpOAuthCredentialSecret{Type: credentialAuthTypeMCPOAuth, AccessToken: "tok"}
		if hasMCPOAuthRefreshMaterial(auth, secret) {
			t.Fatal("expected false without refresh material")
		}
	})
	t.Run("success complete refresh material", func(t *testing.T) {
		auth := &mcpOAuthCredentialAuth{
			Type:         credentialAuthTypeMCPOAuth,
			MCPServerURL: "https://mcp.example.com/mcp",
			Refresh: &mcpOAuthRefresh{
				TokenEndpoint:     "https://example.com/token",
				ClientID:          "client",
				TokenEndpointAuth: tokenEndpointAuth{Type: "none"},
			},
		}
		secret := mcpOAuthCredentialSecret{
			Type:        credentialAuthTypeMCPOAuth,
			AccessToken: "tok",
			Refresh: &mcpOAuthRefreshSecret{
				RefreshToken: "refresh",
				TokenEndpointAuth: &tokenEndpointAuthSecret{Type: "none"},
			},
		}
		if !hasMCPOAuthRefreshMaterial(auth, secret) {
			t.Fatal("expected true with complete refresh material")
		}
	})
}

func TestExchangeMCPOAuthRefreshKeepsRefreshTokenWhenOmitted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "grant_type=refresh_token") {
			t.Fatalf("body = %q", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	publicAuth := &mcpOAuthCredentialAuth{
		Type:         credentialAuthTypeMCPOAuth,
		MCPServerURL: "https://mcp.example.com/mcp",
		ExpiresAt:    strPtr("2020-01-01T00:00:00Z"),
		Refresh: &mcpOAuthRefresh{
			TokenEndpoint:     server.URL,
			ClientID:          "client",
			TokenEndpointAuth: tokenEndpointAuth{Type: "none"},
		},
	}
	secret := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: "old-access",
		Refresh: &mcpOAuthRefreshSecret{
			RefreshToken:      "old-refresh",
			TokenEndpointAuth: &tokenEndpointAuthSecret{Type: "none"},
		},
	}
	token, nextAuth, nextSecret, err := exchangeMCPOAuthRefresh(t.Context(), server.Client(), publicAuth, secret, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q", token)
	}
	savedSecret, err := decodeMCPOAuthCredentialSecret(nextSecret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if savedSecret.Refresh == nil || savedSecret.Refresh.RefreshToken != "old-refresh" {
		t.Fatalf("refresh_token = %+v, want old-refresh preserved", savedSecret.Refresh)
	}
	savedAuth, err := decodeMCPOAuthCredentialAuth(nextAuth)
	if err != nil {
		t.Fatalf("decode auth: %v", err)
	}
	if savedAuth.ExpiresAt == nil || strings.TrimSpace(*savedAuth.ExpiresAt) == "" {
		t.Fatal("expected expires_at to be set from expires_in")
	}
}

func TestResolveExpiresAtAfterRefresh(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	t.Run("failure expired previous cleared without expires_in", func(t *testing.T) {
		got := resolveExpiresAtAfterRefresh(now, strPtr("2026-08-10T11:00:00Z"), nil)
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
	t.Run("failure invalid previous cleared without expires_in", func(t *testing.T) {
		got := resolveExpiresAtAfterRefresh(now, strPtr("not-a-time"), nil)
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
	t.Run("success expires_in updates expires_at", func(t *testing.T) {
		got := resolveExpiresAtAfterRefresh(now, strPtr("2026-08-10T11:00:00Z"), float64(3600))
		if got == nil || *got != "2026-08-10T13:00:00Z" {
			t.Fatalf("got %v, want 2026-08-10T13:00:00Z", got)
		}
	})
	t.Run("success unexpired previous preserved without expires_in", func(t *testing.T) {
		previous := strPtr("2026-08-10T13:00:00Z")
		got := resolveExpiresAtAfterRefresh(now, previous, nil)
		if got != previous {
			t.Fatalf("got %v, want previous pointer retained", got)
		}
	})
}

func TestExchangeMCPOAuthRefreshExpiresAtPolicy(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	newServer := func(payload map[string]any) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(payload)
		}))
	}
	baseAuth := func(serverURL string, expiresAt *string) *mcpOAuthCredentialAuth {
		return &mcpOAuthCredentialAuth{
			Type:         credentialAuthTypeMCPOAuth,
			MCPServerURL: "https://mcp.example.com/mcp",
			ExpiresAt:    expiresAt,
			Refresh: &mcpOAuthRefresh{
				TokenEndpoint:     serverURL,
				ClientID:          "client",
				TokenEndpointAuth: tokenEndpointAuth{Type: "none"},
			},
		}
	}
	secret := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: "old-access",
		Refresh: &mcpOAuthRefreshSecret{
			RefreshToken:      "old-refresh",
			TokenEndpointAuth: &tokenEndpointAuthSecret{Type: "none"},
		},
	}

	t.Run("failure past expires_at cleared when response omits expires_in", func(t *testing.T) {
		server := newServer(map[string]any{"access_token": "new-access"})
		defer server.Close()
		_, nextAuth, _, err := exchangeMCPOAuthRefresh(t.Context(), server.Client(), baseAuth(server.URL, strPtr("2020-01-01T00:00:00Z")), secret, now)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		savedAuth, err := decodeMCPOAuthCredentialAuth(nextAuth)
		if err != nil {
			t.Fatalf("decode auth: %v", err)
		}
		if savedAuth.ExpiresAt != nil {
			t.Fatalf("expires_at = %v, want nil", savedAuth.ExpiresAt)
		}
	})
	t.Run("success future expires_at preserved when response omits expires_in", func(t *testing.T) {
		server := newServer(map[string]any{"access_token": "new-access"})
		defer server.Close()
		previous := strPtr("2026-08-10T18:00:00Z")
		_, nextAuth, _, err := exchangeMCPOAuthRefresh(t.Context(), server.Client(), baseAuth(server.URL, previous), secret, now)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		savedAuth, err := decodeMCPOAuthCredentialAuth(nextAuth)
		if err != nil {
			t.Fatalf("decode auth: %v", err)
		}
		if savedAuth.ExpiresAt == nil || *savedAuth.ExpiresAt != *previous {
			t.Fatalf("expires_at = %v, want %v", savedAuth.ExpiresAt, previous)
		}
	})
}

func TestRefreshMCPOAuthCredentialReusesWinnerAfterCASConflict(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	tokenCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "loser-should-not-persist",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	svc := newTestSecretsService(t)
	stale := sealedMCPOAuthCredential(t, svc, server.URL, "stale-access", "refresh-token", strPtr("2020-01-01T00:00:00Z"))
	winner := sealedMCPOAuthCredential(t, svc, server.URL, "winner-access", "refresh-token", strPtr("2026-08-10T18:00:00Z"))

	store := &fakeCredentialStore{
		updateErr:  db.ErrVersionConflict,
		get:        winner,
		getResults: []db.VaultCredential{stale},
	}
	injector := newTestInjector(t, svc, store, server.Client(), now)

	// force=true models the 401 path; entry reload misses winner, then after CAS
	// conflict force must clear so the winner's unexpired token is reused.
	token, saved, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, now, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if token != "winner-access" {
		t.Fatalf("token = %q, want winner-access", token)
	}
	if saved == nil || saved.ExternalID != winner.ExternalID {
		t.Fatalf("saved = %+v", saved)
	}
	if tokenCalls != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", tokenCalls)
	}
	if store.updateCalls != 1 || store.getCalls != 2 {
		t.Fatalf("updateCalls=%d getCalls=%d", store.updateCalls, store.getCalls)
	}
}

func TestRefreshMCPOAuthCredentialReloadsAfterExchangeFailure(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	tokenCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls++
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
	}))
	defer server.Close()

	svc := newTestSecretsService(t)
	stale := sealedMCPOAuthCredential(t, svc, server.URL, "stale-access", "consumed-refresh", strPtr("2020-01-01T00:00:00Z"))
	winner := sealedMCPOAuthCredential(t, svc, server.URL, "winner-access", "consumed-refresh", strPtr("2026-08-10T18:00:00Z"))

	store := &fakeCredentialStore{
		get:        winner,
		getResults: []db.VaultCredential{stale},
	}
	injector := newTestInjector(t, svc, store, server.Client(), now)

	// 401 path: entry reload still sees stale; exchange then fails invalid_grant
	// because winner consumed the one-time refresh_token; reload reuses winner.
	token, saved, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, now, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if token != "winner-access" {
		t.Fatalf("token = %q, want winner-access", token)
	}
	if saved == nil || saved.ExternalID != winner.ExternalID {
		t.Fatalf("saved = %+v", saved)
	}
	if tokenCalls != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", tokenCalls)
	}
	if store.updateCalls != 0 {
		t.Fatalf("updateCalls=%d, want 0 (no write on exchange failure)", store.updateCalls)
	}
	if store.getCalls != 2 {
		t.Fatalf("getCalls=%d, want 2 (entry reload + post-exchange reload)", store.getCalls)
	}
}

func TestRefreshMCPOAuthCredentialConcurrentExchangeOnce(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	var tokenCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-access", "expires_in": 3600})
	}))
	defer server.Close()

	svc := newTestSecretsService(t)
	stale := sealedMCPOAuthCredential(t, svc, server.URL, "stale-access", "refresh-token", strPtr("2020-01-01T00:00:00Z"))
	winner := sealedMCPOAuthCredential(t, svc, server.URL, "winner-access", "refresh-token", strPtr("2026-08-10T18:00:00Z"))
	// Entry reload is still stale; the loser reloads again under the lock and
	// sees the winner's fresh token without a second exchange.
	store := &fakeCredentialStore{getResults: []db.VaultCredential{stale, winner, winner}}
	injector := newTestInjector(t, svc, store, server.Client(), now)

	var wg sync.WaitGroup
	results := make([]string, 2)
	for idx := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token, _, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, now, false)
			if err != nil {
				results[i] = "error:" + err.Error()
				return
			}
			results[i] = token
		}(idx)
	}
	wg.Wait()

	for _, token := range results {
		if token != "fresh-access" && token != "winner-access" {
			t.Fatalf("result = %q", token)
		}
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", got)
	}
}

func TestRefreshMCPOAuthCredentialRetainsLock(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-access", "expires_in": 3600})
	}))
	defer server.Close()

	svc := newTestSecretsService(t)
	stale := sealedMCPOAuthCredential(t, svc, server.URL, "stale-access", "refresh-token", strPtr("2020-01-01T00:00:00Z"))
	store := &fakeCredentialStore{getResults: []db.VaultCredential{stale}}
	injector := newTestInjector(t, svc, store, server.Client(), now)

	if _, _, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, now, false); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, ok := injector.refreshLocks.Load(stale.ExternalID); !ok {
		t.Fatalf("refreshLocks missing %q after refresh (mutex must be retained)", stale.ExternalID)
	}
}

func sealedMCPOAuthCredential(
	t *testing.T,
	svc *secrets.Service,
	tokenEndpoint string,
	accessToken string,
	refreshToken string,
	expiresAt *string,
) db.VaultCredential {
	t.Helper()
	auth, err := json.Marshal(mcpOAuthCredentialAuth{
		Type:         credentialAuthTypeMCPOAuth,
		MCPServerURL: "https://mcp.example.com/mcp",
		ExpiresAt:    expiresAt,
		Refresh: &mcpOAuthRefresh{
			TokenEndpoint:     tokenEndpoint,
			ClientID:          "client",
			TokenEndpointAuth: tokenEndpointAuth{Type: "none"},
		},
	})
	if err != nil {
		t.Fatalf("marshal auth: %v", err)
	}
	secret, err := json.Marshal(mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: accessToken,
		Refresh: &mcpOAuthRefreshSecret{
			RefreshToken:      refreshToken,
			TokenEndpointAuth: &tokenEndpointAuthSecret{Type: "none"},
		},
	})
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	credential := db.VaultCredential{
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
		VaultExternalID:  "vlt_test",
		ExternalID:       "cred_" + accessToken,
		AuthType:         "mcp_oauth",
		Auth:             auth,
		SecretPayload:    secret,
	}
	if err := SealCredentialSecret(context.Background(), svc, &credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	return credential
}

func strPtr(value string) *string {
	return &value
}

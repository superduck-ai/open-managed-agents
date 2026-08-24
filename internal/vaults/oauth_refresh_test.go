package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
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
	now := oauthRefreshNow()
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
		auth := &mcpOAuthCredentialAuth{Type: credentialAuthTypeMCPOAuth, MCPServerURL: testInjectMCPURL}
		secret := mcpOAuthCredentialSecret{Type: credentialAuthTypeMCPOAuth, AccessToken: "tok"}
		if hasMCPOAuthRefreshMaterial(auth, secret) {
			t.Fatal("expected false without refresh material")
		}
	})
	t.Run("success complete refresh material", func(t *testing.T) {
		auth := testMCPOAuthPublicAuth("https://example.com/token", nil)
		secret := testMCPOAuthSecret("tok", "refresh")
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

	token, nextAuth, nextSecret, err := exchangeMCPOAuthRefresh(
		t.Context(),
		server.Client(),
		testMCPOAuthPublicAuth(server.URL, strPtr("2020-01-01T00:00:00Z")),
		testMCPOAuthSecret("old-access", "old-refresh"),
		oauthRefreshNow(),
		nil,
	)
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
	now := oauthRefreshNow()
	t.Run("failure expired previous cleared without expires_in", func(t *testing.T) {
		got := resolveExpiresAtAfterRefresh(now, strPtr("2026-08-10T11:00:00Z"), 0)
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
	t.Run("failure invalid previous cleared without expires_in", func(t *testing.T) {
		got := resolveExpiresAtAfterRefresh(now, strPtr("not-a-time"), 0)
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
	t.Run("success expires_in updates expires_at", func(t *testing.T) {
		got := resolveExpiresAtAfterRefresh(now, strPtr("2026-08-10T11:00:00Z"), 3600)
		if got == nil || *got != "2026-08-10T13:00:00Z" {
			t.Fatalf("got %v, want 2026-08-10T13:00:00Z", got)
		}
	})
	t.Run("success unexpired previous preserved without expires_in", func(t *testing.T) {
		previous := strPtr("2026-08-10T13:00:00Z")
		got := resolveExpiresAtAfterRefresh(now, previous, 0)
		if got != previous {
			t.Fatalf("got %v, want previous pointer retained", got)
		}
	})
}

func TestOAuthExpiresInUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want OAuthExpiresIn
	}{
		{name: "null", raw: `null`, want: 0},
		{name: "number", raw: `3600`, want: 3600},
		{name: "string number", raw: `"7200"`, want: 7200},
		{name: "empty string", raw: `""`, want: 0},
		{name: "invalid string", raw: `"abc"`, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got OAuthExpiresIn
			if err := json.Unmarshal([]byte(tt.raw), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExchangeMCPOAuthRefreshExpiresAtPolicy(t *testing.T) {
	now := oauthRefreshNow()
	secret := testMCPOAuthSecret("old-access", "old-refresh")
	exchange := func(t *testing.T, expiresAt *string) *mcpOAuthCredentialAuth {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access"})
		}))
		t.Cleanup(server.Close)
		_, nextAuth, _, err := exchangeMCPOAuthRefresh(
			t.Context(),
			server.Client(),
			testMCPOAuthPublicAuth(server.URL, expiresAt),
			secret,
			now,
			nil,
		)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		savedAuth, err := decodeMCPOAuthCredentialAuth(nextAuth)
		if err != nil {
			t.Fatalf("decode auth: %v", err)
		}
		return savedAuth
	}

	t.Run("failure past expires_at cleared when response omits expires_in", func(t *testing.T) {
		savedAuth := exchange(t, strPtr("2020-01-01T00:00:00Z"))
		if savedAuth.ExpiresAt != nil {
			t.Fatalf("expires_at = %v, want nil", savedAuth.ExpiresAt)
		}
	})
	t.Run("success future expires_at preserved when response omits expires_in", func(t *testing.T) {
		previous := strPtr("2026-08-10T18:00:00Z")
		savedAuth := exchange(t, previous)
		if savedAuth.ExpiresAt == nil || *savedAuth.ExpiresAt != *previous {
			t.Fatalf("expires_at = %v, want %v", savedAuth.ExpiresAt, previous)
		}
	})
}

func TestExchangeMCPOAuthRefreshResolvesClientSecret(t *testing.T) {
	now := oauthRefreshNow()
	clients := []config.PlatformOAuthClientConfig{{
		MCPServerURL: testInjectMCPURL,
		ClientID:     "client",
		ClientSecret: "platform-secret",
	}}

	t.Run("failure platform registry miss", func(t *testing.T) {
		auth := testMCPOAuthConfidentialAuth("https://auth.example.com/token", strPtr("2020-01-01T00:00:00Z"), "")
		secret := testMCPOAuthConfidentialSecret("old-access", "old-refresh", "")
		_, _, _, err := exchangeMCPOAuthRefresh(t.Context(), http.DefaultClient, auth, secret, now, nil)
		if !errors.Is(err, errMCPOAuthPlatformClientMissing) {
			t.Fatalf("error = %v, want errMCPOAuthPlatformClientMissing", err)
		}
	})

	t.Run("success platform secret from config stays out of envelope", func(t *testing.T) {
		var sawSecret string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			sawSecret = r.Form.Get("client_secret")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "expires_in": 3600})
		}))
		t.Cleanup(server.Close)

		auth := testMCPOAuthConfidentialAuth(server.URL, strPtr("2020-01-01T00:00:00Z"), MCPOAuthClientCredentialPlatform)
		secret := testMCPOAuthConfidentialSecret("old-access", "old-refresh", "")
		_, nextAuth, nextSecret, err := exchangeMCPOAuthRefresh(t.Context(), server.Client(), auth, secret, now, clients)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		if sawSecret != "platform-secret" {
			t.Fatalf("posted client_secret = %q, want platform-secret", sawSecret)
		}
		savedAuth, err := decodeMCPOAuthCredentialAuth(nextAuth)
		if err != nil {
			t.Fatalf("decode auth: %v", err)
		}
		if savedAuth.ClientCredentialSource != MCPOAuthClientCredentialPlatform {
			t.Fatalf("source = %q, want platform", savedAuth.ClientCredentialSource)
		}
		savedSecret, err := decodeMCPOAuthCredentialSecret(nextSecret)
		if err != nil {
			t.Fatalf("decode secret: %v", err)
		}
		if savedSecret.Refresh == nil || savedSecret.Refresh.TokenEndpointAuth == nil {
			t.Fatal("expected token_endpoint_auth after refresh")
		}
		if savedSecret.Refresh.TokenEndpointAuth.ClientSecret != "" {
			t.Fatalf("persisted client_secret = %q, want empty", savedSecret.Refresh.TokenEndpointAuth.ClientSecret)
		}
	})

	t.Run("success sealed uses envelope secret", func(t *testing.T) {
		var sawSecret string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			sawSecret = r.Form.Get("client_secret")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access"})
		}))
		t.Cleanup(server.Close)

		auth := testMCPOAuthConfidentialAuth(server.URL, strPtr("2020-01-01T00:00:00Z"), MCPOAuthClientCredentialSealed)
		secret := testMCPOAuthConfidentialSecret("old-access", "old-refresh", "byo-secret")
		_, _, nextSecret, err := exchangeMCPOAuthRefresh(t.Context(), server.Client(), auth, secret, now, clients)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		if sawSecret != "byo-secret" {
			t.Fatalf("posted client_secret = %q, want byo-secret", sawSecret)
		}
		savedSecret, err := decodeMCPOAuthCredentialSecret(nextSecret)
		if err != nil {
			t.Fatalf("decode secret: %v", err)
		}
		if savedSecret.Refresh.TokenEndpointAuth.ClientSecret != "byo-secret" {
			t.Fatalf("persisted client_secret = %q, want byo-secret", savedSecret.Refresh.TokenEndpointAuth.ClientSecret)
		}
	})
}

func TestRefreshMCPOAuthCredentialResolvesPlatformClientSecret(t *testing.T) {
	var sawSecret string
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		sawSecret = r.Form.Get("client_secret")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-access", "expires_in": 3600})
	}))
	t.Cleanup(server.Close)

	svc := newTestSecretsService(t)
	stale := sealedConfidentialMCPOAuthCredential(t, svc, server.URL, "stale-access", "refresh-token", "", strPtr("2020-01-01T00:00:00Z"))
	store := &fakeCredentialStore{getResults: []db.VaultCredential{stale}}
	injector := newTestInjector(t, svc, store, server.Client(), oauthRefreshNow()).
		WithPlatformOAuthClients([]config.PlatformOAuthClientConfig{{
			MCPServerURL: testInjectMCPURL,
			ClientID:     "client",
			ClientSecret: "platform-secret",
		}})

	token, saved, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, oauthRefreshNow(), false)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if token != "fresh-access" {
		t.Fatalf("token = %q", token)
	}
	if sawSecret != "platform-secret" {
		t.Fatalf("posted client_secret = %q, want platform-secret", sawSecret)
	}
	if calls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls.Load())
	}
	if saved == nil {
		t.Fatal("expected saved credential")
	}
}

func TestRefreshMCPOAuthCredentialPlatformSecretMissing(t *testing.T) {
	svc := newTestSecretsService(t)
	stale := sealedConfidentialMCPOAuthCredential(t, svc, "https://auth.example.com/token", "stale-access", "refresh-token", "", strPtr("2020-01-01T00:00:00Z"))
	store := &fakeCredentialStore{getResults: []db.VaultCredential{stale}}
	injector := newTestInjector(t, svc, store, http.DefaultClient, oauthRefreshNow())

	_, _, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, oauthRefreshNow(), false)
	if !errors.Is(err, errMCPOAuthPlatformClientMissing) {
		t.Fatalf("error = %v, want errMCPOAuthPlatformClientMissing", err)
	}
}

func TestRefreshMCPOAuthCredentialReusesWinnerAfterCASConflict(t *testing.T) {
	env := newOAuthRefreshEnv(t, "loser-should-not-persist")
	stale, winner := env.staleWinner("refresh-token", false)
	store := &fakeCredentialStore{
		updateErr:  db.ErrVersionConflict,
		get:        winner,
		getResults: []db.VaultCredential{stale},
	}
	injector := env.injector(store)

	// force=true models the 401 path; entry reload misses winner, then after CAS
	// conflict force must clear so the winner's unexpired token is reused.
	token, saved, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, env.now, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if token != "winner-access" {
		t.Fatalf("token = %q, want winner-access", token)
	}
	if saved == nil || saved.ExternalID != winner.ExternalID {
		t.Fatalf("saved = %+v", saved)
	}
	if env.tokenCalls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", env.tokenCalls.Load())
	}
	if store.updateCalls != 1 || store.getCalls != 2 {
		t.Fatalf("updateCalls=%d getCalls=%d", store.updateCalls, store.getCalls)
	}
}

func TestRefreshMCPOAuthCredentialReloadsAfterExchangeFailure(t *testing.T) {
	env := newOAuthRefreshEnv(t, "")
	stale, winner := env.staleWinner("consumed-refresh", true)
	store := &fakeCredentialStore{
		get:        winner,
		getResults: []db.VaultCredential{stale},
	}
	injector := env.injector(store)

	// 401 path: entry reload still sees stale; exchange then fails invalid_grant
	// because winner consumed the one-time refresh_token; reload reuses winner.
	token, saved, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, env.now, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if token != "winner-access" {
		t.Fatalf("token = %q, want winner-access", token)
	}
	if saved == nil || saved.ExternalID != winner.ExternalID {
		t.Fatalf("saved = %+v", saved)
	}
	if env.tokenCalls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", env.tokenCalls.Load())
	}
	if store.updateCalls != 0 {
		t.Fatalf("updateCalls=%d, want 0 (no write on exchange failure)", store.updateCalls)
	}
	if store.getCalls != 2 {
		t.Fatalf("getCalls=%d, want 2 (entry reload + post-exchange reload)", store.getCalls)
	}
}

func TestRefreshMCPOAuthCredentialKeepsExchangeErrorWhenVersionUnchanged(t *testing.T) {
	env := newOAuthRefreshEnv(t, "")
	stale := env.staleCred("bad-refresh")
	store := &fakeCredentialStore{getResults: []db.VaultCredential{stale, stale}}
	injector := env.injector(store)

	_, _, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, env.now, true)
	if err == nil {
		t.Fatal("expected exchange error when reload does not advance SecretVersion")
	}
	if env.tokenCalls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1 (no retry exchange)", env.tokenCalls.Load())
	}
}

func TestRefreshMCPOAuthCredentialConcurrentExchangeOnce(t *testing.T) {
	env := newOAuthRefreshEnv(t, "fresh-access")
	stale, winner := env.staleWinner("refresh-token", false)
	// Entry reload is still stale; the loser reloads again under the lock and
	// sees the winner's fresh token without a second exchange.
	store := &fakeCredentialStore{getResults: []db.VaultCredential{stale, winner, winner}}
	injector := env.injector(store)

	var wg sync.WaitGroup
	results := make([]string, 2)
	for idx := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token, _, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, env.now, false)
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
	if got := env.tokenCalls.Load(); got != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", got)
	}
}

func TestRefreshMCPOAuthCredentialUsesLeaseBeforeExchange(t *testing.T) {
	env := newOAuthRefreshEnv(t, "fresh-access")
	stale := env.staleCred("refresh-token")
	store := &fakeCredentialStore{getResults: []db.VaultCredential{stale}}
	injector := env.injector(store)
	blocker := &blockingOAuthRefreshLease{
		held:    make(chan struct{}),
		release: make(chan struct{}),
	}
	injector.refreshLease = blocker

	done := make(chan error, 1)
	go func() {
		_, _, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, env.now, false)
		done <- err
	}()
	select {
	case <-blocker.held:
	case <-time.After(time.Second):
		t.Fatal("refresh did not acquire lease")
	}
	if got := env.tokenCalls.Load(); got != 0 {
		t.Fatalf("token endpoint calls = %d before lease release, want 0", got)
	}
	close(blocker.release)
	if err := <-done; err != nil {
		t.Fatalf("refresh: %v", err)
	}
}

type blockingOAuthRefreshLease struct {
	held    chan struct{}
	release chan struct{}
	once    sync.Once
}

func (l *blockingOAuthRefreshLease) Hold(context.Context, string) (func() error, error) {
	l.once.Do(func() { close(l.held) })
	<-l.release
	return func() error { return nil }, nil
}

func oauthRefreshNow() time.Time {
	return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
}

func testMCPOAuthPublicAuth(tokenURL string, expiresAt *string) *mcpOAuthCredentialAuth {
	return &mcpOAuthCredentialAuth{
		Type:         credentialAuthTypeMCPOAuth,
		MCPServerURL: testInjectMCPURL,
		ExpiresAt:    expiresAt,
		Refresh: &mcpOAuthRefresh{
			TokenEndpoint:     tokenURL,
			ClientID:          "client",
			TokenEndpointAuth: tokenEndpointAuth{Type: "none"},
		},
	}
}

func testMCPOAuthConfidentialAuth(tokenURL string, expiresAt *string, source string) *mcpOAuthCredentialAuth {
	auth := testMCPOAuthPublicAuth(tokenURL, expiresAt)
	auth.ClientCredentialSource = source
	auth.Refresh.TokenEndpointAuth = tokenEndpointAuth{Type: "client_secret_post"}
	return auth
}

func testMCPOAuthSecret(accessToken, refreshToken string) mcpOAuthCredentialSecret {
	return mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: accessToken,
		Refresh: &mcpOAuthRefreshSecret{
			RefreshToken:      refreshToken,
			TokenEndpointAuth: &tokenEndpointAuthSecret{Type: "none"},
		},
	}
}

func testMCPOAuthConfidentialSecret(accessToken, refreshToken, clientSecret string) mcpOAuthCredentialSecret {
	return mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: accessToken,
		Refresh: &mcpOAuthRefreshSecret{
			RefreshToken: refreshToken,
			TokenEndpointAuth: &tokenEndpointAuthSecret{
				Type:         "client_secret_post",
				ClientSecret: clientSecret,
			},
		},
	}
}

// oauthRefreshEnv is the shared token-endpoint + secrets + clock seam for refresh tests.
type oauthRefreshEnv struct {
	t          *testing.T
	now        time.Time
	svc        *secrets.Service
	tokenURL   string
	client     *http.Client
	tokenCalls *atomic.Int32
}

// newOAuthRefreshEnv starts a token server. Empty accessToken yields invalid_grant.
func newOAuthRefreshEnv(t *testing.T, accessToken string) *oauthRefreshEnv {
	t.Helper()
	var server *httptest.Server
	var calls *atomic.Int32
	if accessToken == "" {
		server, calls = newOAuthInvalidGrantServer(t)
	} else {
		server, calls = newOAuthAccessTokenServer(t, accessToken)
	}
	return &oauthRefreshEnv{
		t:          t,
		now:        oauthRefreshNow(),
		svc:        newTestSecretsService(t),
		tokenURL:   server.URL,
		client:     server.Client(),
		tokenCalls: calls,
	}
}

func (e *oauthRefreshEnv) injector(store *fakeCredentialStore) *Injector {
	e.t.Helper()
	return newTestInjector(e.t, e.svc, store, e.client, e.now)
}

func (e *oauthRefreshEnv) staleCred(refreshToken string) db.VaultCredential {
	e.t.Helper()
	return sealedMCPOAuthCredential(e.t, e.svc, e.tokenURL, "stale-access", refreshToken, strPtr("2020-01-01T00:00:00Z"))
}

func (e *oauthRefreshEnv) staleWinner(refreshToken string, bumpWinnerVersion bool) (db.VaultCredential, db.VaultCredential) {
	e.t.Helper()
	stale := e.staleCred(refreshToken)
	winner := sealedMCPOAuthCredential(e.t, e.svc, e.tokenURL, "winner-access", refreshToken, strPtr("2026-08-10T18:00:00Z"))
	if bumpWinnerVersion {
		winner.SecretVersion = stale.SecretVersion + 1
	}
	return stale, winner
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
	return sealMCPOAuthCredential(t, svc, testMCPOAuthPublicAuth(tokenEndpoint, expiresAt), testMCPOAuthSecret(accessToken, refreshToken), accessToken)
}

func sealedConfidentialMCPOAuthCredential(
	t *testing.T,
	svc *secrets.Service,
	tokenEndpoint string,
	accessToken string,
	refreshToken string,
	clientSecret string,
	expiresAt *string,
) db.VaultCredential {
	t.Helper()
	return sealMCPOAuthCredential(
		t,
		svc,
		testMCPOAuthConfidentialAuth(tokenEndpoint, expiresAt, ""),
		testMCPOAuthConfidentialSecret(accessToken, refreshToken, clientSecret),
		accessToken,
	)
}

func sealMCPOAuthCredential(
	t *testing.T,
	svc *secrets.Service,
	auth *mcpOAuthCredentialAuth,
	secret mcpOAuthCredentialSecret,
	accessToken string,
) db.VaultCredential {
	t.Helper()
	authJSON, err := json.Marshal(auth)
	if err != nil {
		t.Fatalf("marshal auth: %v", err)
	}
	secretJSON, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	credential := db.VaultCredential{
		OrganizationUUID: testInjectOrgUUID,
		WorkspaceUUID:    testInjectWsUUID,
		VaultExternalID:  "vlt_test",
		ExternalID:       "cred_" + accessToken,
		AuthType:         "mcp_oauth",
		Auth:             authJSON,
		SecretPayload:    secretJSON,
	}
	if err := SealCredentialSecret(context.Background(), svc, &credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	return credential
}

func strPtr(value string) *string {
	return &value
}

package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type fakeEnvOAuthProviderResolver struct {
	snapshot EnvOAuthProviderSnapshot
	err      error
}

func (f fakeEnvOAuthProviderResolver) ResolveEnvOAuthProvider(context.Context, string) (EnvOAuthProviderSnapshot, error) {
	return f.snapshot, f.err
}

func sealedEnvOAuthCredential(t *testing.T, svc *secrets.Service, secretName, placeholder, host, accessToken, refreshToken string, expiresAt *string) db.VaultCredential {
	t.Helper()
	auth, err := json.Marshal(map[string]any{
		"type":               "env_oauth",
		"provider":           EnvOAuthProviderBuiltinFeishu,
		"secret_name":        secretName,
		"placeholder":        placeholder,
		"networking":         map[string]any{"type": "limited", "allowed_hosts": []string{host}},
		"injection_location": map[string]any{"header": true, "body": false},
		"expires_at":         expiresAt,
		"has_refresh_token":  refreshToken != "",
	})
	if err != nil {
		t.Fatalf("marshal auth: %v", err)
	}
	secret, err := json.Marshal(map[string]any{
		"type":          "env_oauth",
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	credential := db.VaultCredential{
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
		VaultExternalID:  "vlt_" + secretName,
		ExternalID:       "cred_" + secretName,
		AuthType:         "env_oauth",
		CredentialKey:    secretName,
		Auth:             auth,
		SecretPayload:    secret,
	}
	if err := SealCredentialSecret(context.Background(), svc, &credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	return credential
}

func TestSubstituteEnvOAuthSecrets(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	credential := sealedEnvOAuthCredential(t, svc, EnvOAuthFeishuDefaultSecretName, "oma_ph_feishu", "open.feishu.cn", "u-real", "", nil)
	store := &fakeCredentialStore{vaultIDs: []string{"vlt_a"}, credentials: []db.VaultCredential{credential}, get: credential}
	substitutor := newEgressSubstitutor(store, svc, nil)
	substitutor.setEnvOAuthTokens(newEnvOAuthTokenResolver(store, svc, nil, nil))

	req, err := http.NewRequest(http.MethodGet, "https://open.feishu.cn/open-apis/docx", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_feishu")
	if err := substitutor.SubstituteEnvSecrets(context.Background(), "cse_test", credential.OrganizationUUID, credential.WorkspaceUUID, req, "open.feishu.cn", "443"); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer u-real" {
		t.Fatalf("Authorization = %q", got)
	}

	req, err = http.NewRequest(http.MethodGet, "https://other.example.com/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_feishu")
	if err := substitutor.SubstituteEnvSecrets(context.Background(), "cse_test", credential.OrganizationUUID, credential.WorkspaceUUID, req, "other.example.com", "443"); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer oma_ph_feishu" {
		t.Fatalf("uncovered host Authorization = %q", got)
	}
}

func TestSubstituteEnvOAuthSecretsPrefersEnvironmentVariable(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	envCredential := sealedEnvCredential(t, svc, "SHARED", "oma_ph_env", "open.feishu.cn", "env-real")
	oauthCredential := sealedEnvOAuthCredential(t, svc, "SHARED", "oma_ph_oauth", "open.feishu.cn", "oauth-real", "", nil)
	store := &fakeCredentialStore{vaultIDs: []string{"vlt_a", "vlt_b"}, credentials: []db.VaultCredential{envCredential, oauthCredential}, get: oauthCredential}
	substitutor := newEgressSubstitutor(store, svc, nil)
	substitutor.setEnvOAuthTokens(newEnvOAuthTokenResolver(store, svc, nil, nil))
	req, err := http.NewRequest(http.MethodGet, "https://open.feishu.cn/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Env", "oma_ph_env")
	req.Header.Set("X-OAuth", "oma_ph_oauth")
	if err := substitutor.SubstituteEnvSecrets(context.Background(), "cse_test", envCredential.OrganizationUUID, envCredential.WorkspaceUUID, req, "open.feishu.cn", "443"); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("X-Env"); got != "env-real" {
		t.Fatalf("X-Env = %q", got)
	}
	if got := req.Header.Get("X-OAuth"); got != "oma_ph_oauth" {
		t.Fatalf("X-OAuth = %q, want untouched placeholder", got)
	}
}

func TestEnvOAuthTokenResolverRefreshesExpiredToken(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	credential := sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "open.feishu.cn", "expired-access", "refresh-1", &past)
	var tokenForm map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenForm = map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&tokenForm)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh-access",
			"refresh_token": "refresh-2",
			"expires_in":    3600,
			"scope":         "offline_access",
		})
	}))
	defer server.Close()

	store := &fakeCredentialStore{get: credential}
	resolver := newEnvOAuthTokenResolver(store, svc, nil, nil).
		withHTTPClient(server.Client()).
		withProviders(fakeEnvOAuthProviderResolver{snapshot: EnvOAuthProviderSnapshot{
			ProviderKey:             EnvOAuthProviderBuiltinFeishu,
			TokenEndpoint:           server.URL,
			ClientID:                "client",
			ClientSecret:            "secret",
			Scope:                   "offline_access",
			TokenEndpointAuthMethod: "client_secret_post",
			TokenEndpointEncoding:   "json",
		}})
	token, err := resolver.resolve(context.Background(), &credential)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if token != "fresh-access" {
		t.Fatalf("token = %q", token)
	}
	if tokenForm["grant_type"] != "refresh_token" || tokenForm["refresh_token"] != "refresh-1" || tokenForm["client_secret"] != "secret" {
		t.Fatalf("token form = %v", tokenForm)
	}
	if store.updateCalls != 1 {
		t.Fatalf("update calls = %d", store.updateCalls)
	}
	plaintext, err := openCredentialSecret(context.Background(), svc, store.lastUpdate)
	if err != nil {
		t.Fatalf("open updated secret: %v", err)
	}
	nextSecret, err := decodeEnvOAuthCredentialSecret(plaintext)
	if err != nil {
		t.Fatalf("decode updated secret: %v", err)
	}
	if nextSecret.AccessToken != "fresh-access" || nextSecret.RefreshToken != "refresh-2" {
		t.Fatalf("updated secret = %+v", nextSecret)
	}
	nextAuth, err := decodeEnvOAuthCredentialAuth(store.lastUpdate.Auth)
	if err != nil {
		t.Fatalf("decode updated auth: %v", err)
	}
	if nextAuth.ExpiresAt == nil || !nextAuth.HasRefreshToken || nextAuth.Scope == nil || *nextAuth.Scope != "offline_access" {
		t.Fatalf("updated auth = %+v", nextAuth)
	}
}

func TestEnvOAuthTokenResolverFailsClosed(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)

	t.Run("no refresh token", func(t *testing.T) {
		credential := sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "open.feishu.cn", "expired", "", &past)
		store := &fakeCredentialStore{get: credential}
		resolver := newEnvOAuthTokenResolver(store, svc, nil, nil)
		_, err := resolver.resolve(context.Background(), &credential)
		if !errors.Is(err, ErrEnvOAuthReauthorizationRequired) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("provider unavailable", func(t *testing.T) {
		credential := sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "open.feishu.cn", "expired", "refresh", &past)
		store := &fakeCredentialStore{get: credential}
		resolver := newEnvOAuthTokenResolver(store, svc, nil, nil)
		_, err := resolver.resolve(context.Background(), &credential)
		if !errors.Is(err, ErrEnvOAuthProviderUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("permanent endpoint failure", func(t *testing.T) {
		credential := sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "open.feishu.cn", "expired", "refresh", &past)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		}))
		defer server.Close()
		store := &fakeCredentialStore{get: credential}
		resolver := newEnvOAuthTokenResolver(store, svc, nil, nil).
			withHTTPClient(server.Client()).
			withProviders(fakeEnvOAuthProviderResolver{snapshot: EnvOAuthProviderSnapshot{
				TokenEndpoint:           server.URL,
				ClientID:                "client",
				TokenEndpointAuthMethod: "none",
			}})
		_, err := resolver.resolve(context.Background(), &credential)
		if !errors.Is(err, ErrEnvOAuthReauthorizationRequired) {
			t.Fatalf("error = %v", err)
		}
		if store.updateCalls != 0 {
			t.Fatalf("update calls = %d, want none", store.updateCalls)
		}
	})
}

func TestSubstituteEnvOAuthSecretsFailsClosedOnRefreshError(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	credential := sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "open.feishu.cn", "expired", "", &past)
	store := &fakeCredentialStore{vaultIDs: []string{"vlt_a"}, credentials: []db.VaultCredential{credential}, get: credential}
	substitutor := newEgressSubstitutor(store, svc, nil)
	substitutor.setEnvOAuthTokens(newEnvOAuthTokenResolver(store, svc, nil, nil))
	req, err := http.NewRequest(http.MethodGet, "https://open.feishu.cn/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_token")
	err = substitutor.SubstituteEnvSecrets(context.Background(), "cse_test", credential.OrganizationUUID, credential.WorkspaceUUID, req, "open.feishu.cn", "443")
	if !errors.Is(err, ErrSubstitutionRejected) || !errors.Is(err, ErrEnvOAuthReauthorizationRequired) {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(req.Header.Get("Authorization"), "oma_ph_token") {
		t.Fatalf("Authorization mutated on failure: %q", req.Header.Get("Authorization"))
	}
}

func TestEnvOAuthRefreshCASPreservesLatestMaterial(t *testing.T) {
	for _, scenario := range []string{"expired access", "refresh source", "networking", "valid access"} {
		t.Run(scenario, func(t *testing.T) {
			svc := newTestSecretsService(t)
			past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
			original := sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "old.example", "old", "refresh", &past)
			latest := original
			latest.SecretVersion++
			latestAuth, err := decodeEnvOAuthCredentialAuth(latest.Auth)
			if err != nil {
				t.Fatal(err)
			}
			hosts := []string{"restricted.example"}
			latestAuth.Networking = credentialAuthNetworking{Type: "limited", AllowedHosts: &hosts}
			latest.Auth, err = json.Marshal(latestAuth)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "networking" {
				access, refresh := "replacement", "refresh"
				if scenario == "refresh source" {
					access, refresh = "old", "replacement-refresh"
				}
				expiry := &past
				if scenario == "valid access" {
					expiry = nil
				}
				latest = sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "restricted.example", access, refresh, expiry)
				latest.SecretVersion = original.SecretVersion + 1
			}
			store := &fakeCredentialStore{get: latest, updateErrs: []error{db.ErrVersionConflict, nil}}
			resolver := newEnvOAuthTokenResolver(store, svc, nil, nil)
			nextAuth, err := decodeEnvOAuthCredentialAuth(original.Auth)
			if err != nil {
				t.Fatal(err)
			}
			nextAuth.ExpiresAt = nil
			token, err := resolver.persist(t.Context(), &original, envOAuthCredentialSecret{AccessToken: "old", RefreshToken: "refresh"}, *nextAuth, json.RawMessage(`{"type":"env_oauth","access_token":"fresh","refresh_token":"fresh-refresh"}`), "fresh", time.Now())
			if scenario == "expired access" || scenario == "refresh source" {
				if !errors.Is(err, errEnvOAuthRefreshUnavailable) || store.updateCalls != 1 {
					t.Fatalf("token=%q err=%v writes=%d", token, err, store.updateCalls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "valid access" {
				if token != "replacement" || store.updateCalls != 1 {
					t.Fatalf("token=%q writes=%d", token, store.updateCalls)
				}
				return
			}
			saved, err := decodeEnvOAuthCredentialAuth(store.lastUpdate.Auth)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Networking.AllowedHosts == nil || (*saved.Networking.AllowedHosts)[0] != "restricted.example" || saved.Placeholder != latestAuth.Placeholder || saved.Provider != latestAuth.Provider || store.updateCalls != 2 {
				t.Fatalf("saved=%+v writes=%d", saved, store.updateCalls)
			}
			_, secret, err := resolver.openMaterial(t.Context(), store.lastUpdate)
			if err != nil || secret.AccessToken != token || token != "fresh" {
				t.Fatalf("token=%q err=%v", token, err)
			}
		})
	}
}

func TestEnvOAuthTokenResolverResamplesClockAfterRefreshLease(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	base := time.Now().UTC().Truncate(time.Second)
	expired := base.Add(-time.Minute).Format(time.RFC3339)
	expiresDuringWait := base.Add(5 * time.Minute).Format(time.RFC3339)
	initial := sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "open.feishu.cn", "stale-access", "refresh-1", &expired)
	reloaded := sealedEnvOAuthCredential(t, svc, "TOKEN", "oma_ph_token", "open.feishu.cn", "stale-access", "refresh-1", &expiresDuringWait)

	var exchanges int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		exchanges++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh-access",
			"refresh_token": "refresh-2",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	store := &fakeCredentialStore{get: reloaded}
	resolver := newEnvOAuthTokenResolver(store, svc, nil, nil).
		withHTTPClient(server.Client()).
		withProviders(fakeEnvOAuthProviderResolver{snapshot: EnvOAuthProviderSnapshot{
			ProviderKey:             EnvOAuthProviderBuiltinFeishu,
			TokenEndpoint:           server.URL,
			ClientID:                "client",
			ClientSecret:            "secret",
			TokenEndpointAuthMethod: "client_secret_post",
			TokenEndpointEncoding:   "json",
		}})
	clockCalls := 0
	resolver.now = func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return base
		}
		return base.Add(10 * time.Minute)
	}

	token, err := resolver.resolve(t.Context(), &initial)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if token != "fresh-access" {
		t.Fatalf("token = %q, want refreshed access token", token)
	}
	if exchanges != 1 || store.updateCalls != 1 {
		t.Fatalf("exchanges = %d, writes = %d", exchanges, store.updateCalls)
	}
}

package vaults

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestMissingOAuthRefreshRequiresAuthenticationRecovery(t *testing.T) {
	for _, row := range []struct {
		name           string
		expired, force bool
	}{{"expired", true, false}, {"remotely rejected", false, true}, {"usable", false, false}} {
		t.Run(row.name, func(t *testing.T) {
			env := newOAuthRefreshEnv(t, "unused-new-token")
			expiry := env.now.Add(time.Hour).Format(time.RFC3339)
			if row.expired {
				expiry = env.now.Add(-time.Hour).Format(time.RFC3339)
			}
			auth := testMCPOAuthPublicAuth(env.tokenURL, &expiry)
			auth.Refresh = nil
			secret := testMCPOAuthSecret("access-only", "")
			secret.Refresh = nil
			cred := sealMCPOAuthCredential(t, env.svc, auth, secret, "access-only")
			store := &fakeCredentialStore{get: cred}
			result, err := env.injector(store).resolveMCPOAuthToken(t.Context(), &cred, row.force)
			if row.expired || row.force {
				if result != nil || !errors.Is(err, ErrMCPOAuthReauthorizationRequired) || !errors.Is(err, errMCPOAuthRefreshTokenMissing) {
					t.Fatalf("missing-refresh authentication classification: %v", err)
				}
			} else if err != nil || result == nil || result.token != "access-only" {
				t.Fatal("valid access-only token unusable")
			}
			if env.tokenCalls.Load() != 0 || store.updateCalls != 0 {
				t.Fatal("missing refresh invented token exchange or mutation")
			}
		})
	}
}

func TestOAuthEndpointRejectionRequiresAuthenticationRecovery(t *testing.T) {
	for _, status := range []int{400, 401, 403, 408, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "synthetic-refresh" {
					t.Error("missing refresh grant")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			}))
			defer server.Close()
			for _, force := range []bool{false, true} {
				env := &oauthRefreshEnv{t: t, now: oauthRefreshNow(), svc: newTestSecretsService(t), tokenURL: server.URL, client: server.Client(), tokenCalls: &calls}
				expiry := env.now.Add(-time.Hour).Format(time.RFC3339)
				if force {
					expiry = env.now.Add(time.Hour).Format(time.RFC3339)
				}
				cred := sealedMCPOAuthCredential(t, env.svc, server.URL, "synthetic-access", "synthetic-refresh", &expiry)
				store := &fakeCredentialStore{get: cred}
				before := calls.Load()
				result, err := env.injector(store).resolveMCPOAuthToken(t.Context(), &cred, force)
				permanent := status == 400 || status == 401 || status == 403
				var endpoint oauthTokenEndpointError
				if result != nil || err == nil || errors.Is(err, ErrMCPOAuthReauthorizationRequired) != permanent || !errors.As(err, &endpoint) || endpoint.status != status {
					t.Fatalf("wrong OAuth error classification for %d: %v", status, err)
				}
				if calls.Load() != before+1 || store.updateCalls != 0 {
					t.Fatal("rejection was not real or mutated credential")
				}
			}
		})
	}
}

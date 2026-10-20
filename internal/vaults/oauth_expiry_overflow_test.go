package vaults

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOAuthRefreshExpiryCannotOverflowDuration(t *testing.T) {
	for _, row := range []struct {
		name, value string
		seconds     int64
		valid       bool
	}{
		{"normal", "3600", 3600, true},
		{"maximum duration", "9223372036", 9223372036, true},
		{"first overflow", "9223372037", 0, false},
		{"observed huge value", "1125899906842624", 0, false},
		{"huge numeric string", `"1125899906842624"`, 0, false},
		{"beyond integer", "9223372036854775808", 0, false},
		{"beyond integer string", `"9223372036854775808"`, 0, false},
		{"exponent overflow", "1e100", 0, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("grant_type") != "refresh_token" {
					t.Error("missing grant")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"access_token":"new-access","token_type":"Bearer","expires_in":%s}`, row.value)
			}))
			defer server.Close()
			env := &oauthRefreshEnv{t: t, now: oauthRefreshNow(), svc: newTestSecretsService(t), tokenURL: server.URL, client: server.Client()}
			cred := env.staleCred("synthetic-refresh")
			store := &fakeCredentialStore{get: cred}
			result, err := env.injector(store).resolveMCPOAuthToken(t.Context(), &cred, false)
			if !row.valid {
				if err == nil || result != nil || store.updateCalls != 0 {
					t.Fatal("overflowing expiry produced injectable or persisted token")
				}
				return
			}
			if err != nil || result == nil || store.updateCalls != 1 {
				t.Fatalf("valid lifetime rejected: %v", err)
			}
			auth, err := decodeMCPOAuthCredentialAuth(store.lastUpdate.Auth)
			if err != nil || auth.ExpiresAt == nil {
				t.Fatal("valid expiry missing")
			}
			expires, err := time.Parse(time.RFC3339, *auth.ExpiresAt)
			if err != nil || !expires.Equal(env.now.Add(time.Duration(row.seconds)*time.Second)) {
				t.Fatal("expiry arithmetic changed")
			}
		})
	}
}

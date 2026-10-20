package vaults

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuthRefreshTokenTypeValidation(t *testing.T) {
	for _, row := range []struct {
		name, field string
		valid       bool
	}{
		{"Bearer", `,"token_type":"Bearer"`, true},
		{"case insensitive", `,"token_type":"bEaReR"`, true},
		{"omitted legacy", ``, true},
		{"Basic", `,"token_type":"Basic"`, false},
		{"number", `,"token_type":17`, false},
		{"null", `,"token_type":null`, false},
		{"empty", `,"token_type":""`, false},
		{"object", `,"token_type":{}`, false},
		{"array", `,"token_type":[]`, false},
		{"malformed trailing field", `,"unknown":`, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "synthetic-refresh" {
					t.Error("missing real refresh grant")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"access_token":"fresh-access","expires_in":3600%s}`, row.field)
			}))
			defer server.Close()
			env := &oauthRefreshEnv{t: t, now: oauthRefreshNow(), svc: newTestSecretsService(t), tokenURL: server.URL, client: server.Client()}
			cred := env.staleCred("synthetic-refresh")
			store := &fakeCredentialStore{get: cred}
			result, err := env.injector(store).resolveMCPOAuthToken(t.Context(), &cred, false)
			if row.valid {
				if err != nil || result == nil || result.token != "fresh-access" || store.updateCalls != 1 {
					t.Fatalf("valid token type rejected: %v", err)
				}
			} else if err == nil || result != nil || store.updateCalls != 0 {
				t.Fatal("unsupported or malformed token response persisted or became injectable")
			}
		})
	}
}

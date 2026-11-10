package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestNormalizeEnvOAuthRedirectURL(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		env       string
		console   string
		value     string
		wantError bool
	}{
		{name: "dev http", env: config.EnvironmentDev, value: "http://oma-dev-web.localhost/oauth/vault/env-success"},
		{name: "prod https", env: config.EnvironmentProd, value: "https://oma.example.test/oauth/vault/env-success"},
		{name: "prod http", env: config.EnvironmentProd, value: "http://oma.example.test/oauth/vault/env-success", wantError: true},
		{name: "console origin match", env: config.EnvironmentProd, console: "https://console.example.test", value: "https://console.example.test/oauth/vault/env-success"},
		{name: "console origin mismatch", env: config.EnvironmentProd, console: "https://console.example.test", value: "https://attacker.example.test/oauth/vault/env-success", wantError: true},
		{name: "console scheme mismatch", env: config.EnvironmentDev, console: "https://console.example.test", value: "http://console.example.test/oauth/vault/env-success", wantError: true},
		{name: "wrong path", env: config.EnvironmentProd, value: "https://oma.example.test/oauth/vault/success", wantError: true},
		{name: "non root path", env: config.EnvironmentDev, value: "http://oma-dev-web.localhost/oauth/vault/env-success/extra", wantError: true},
		{name: "query string", env: config.EnvironmentProd, value: "https://oma.example.test/oauth/vault/env-success?next=https://attacker.example.test", wantError: true},
		{name: "fragment", env: config.EnvironmentProd, value: "https://oma.example.test/oauth/vault/env-success#fragment", wantError: true},
		{name: "userinfo", env: config.EnvironmentProd, value: "https://user@oma.example.test/oauth/vault/env-success", wantError: true},
		{name: "relative", env: config.EnvironmentDev, value: "/oauth/vault/env-success", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := &Server{cfg: config.Config{Env: test.env, Auth: config.AuthConfig{ConsoleURL: test.console}}}
			got, err := server.normalizeEnvOAuthRedirectURL(test.value)
			if test.wantError {
				if err == nil {
					t.Fatalf("redirect %q accepted as %q", test.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("redirect %q rejected: %v", test.value, err)
			}
			if got != test.value {
				t.Fatalf("redirect = %q, want %q", got, test.value)
			}
		})
	}
}

func TestEnvOAuthTokenHTTPClientDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	var replayed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		replayed.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"redirected"}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	_, err := vaultsapi.ExchangeOAuthTokenEndpoint(context.Background(), envOAuthTokenHTTPClient, vaultsapi.OAuthTokenEndpointExchange{
		TokenEndpoint:           source.URL,
		ClientID:                "client",
		ClientSecret:            "secret",
		TokenEndpointAuthMethod: "client_secret_post",
		TokenEndpointEncoding:   "form",
		Form:                    url.Values{"grant_type": []string{"authorization_code"}, "code": []string{"code"}},
	})
	if err == nil {
		t.Fatal("token exchange followed the redirect")
	}
	if replayed.Load() {
		t.Fatal("token endpoint body was replayed to the redirect target")
	}
}

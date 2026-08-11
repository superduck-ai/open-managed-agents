package vaults

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestExchangeOAuthTokenEndpoint(t *testing.T) {
	t.Run("failure missing client secret for basic", func(t *testing.T) {
		_, err := ExchangeOAuthTokenEndpoint(t.Context(), http.DefaultClient, OAuthTokenEndpointExchange{
			TokenEndpoint:           "https://auth.example.com/token",
			ClientID:                "client",
			TokenEndpointAuthMethod: "client_secret_basic",
			Form:                    url.Values{"grant_type": {"refresh_token"}},
		})
		if err == nil || err.Error() != `client_secret_basic selected without client secret` {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("success posts form and decodes token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Fatalf("content-type = %q", r.Header.Get("Content-Type"))
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "abc" {
				t.Fatalf("form = %v", r.Form)
			}
			if r.Form.Get("client_secret") != "secret" {
				t.Fatalf("client_secret = %q", r.Form.Get("client_secret"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access",
				"refresh_token": "refresh",
				"expires_in":    "3600",
				"scope":         "mcp",
			})
		}))
		defer server.Close()

		got, err := ExchangeOAuthTokenEndpoint(t.Context(), server.Client(), OAuthTokenEndpointExchange{
			TokenEndpoint:           server.URL,
			ClientID:                "client",
			ClientSecret:            "secret",
			TokenEndpointAuthMethod: "client_secret_post",
			Form: url.Values{
				"grant_type": {"authorization_code"},
				"code":       {"abc"},
			},
		})
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		if got.AccessToken != "access" || got.RefreshToken != "refresh" || got.Scope != "mcp" || got.ExpiresIn != 3600 {
			t.Fatalf("result = %#v", got)
		}
	})
}

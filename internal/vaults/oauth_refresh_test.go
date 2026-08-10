package vaults

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccessTokenExpired(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	t.Run("failure missing expires_at is not expired", func(t *testing.T) {
		expired, err := accessTokenExpired("", now)
		if err != nil || expired {
			t.Fatalf("expired=%v err=%v, want false nil", expired, err)
		}
	})
	t.Run("failure invalid expires_at", func(t *testing.T) {
		if _, err := accessTokenExpired("not-a-time", now); err == nil {
			t.Fatal("expected parse error")
		}
	})
	t.Run("success past expires_at", func(t *testing.T) {
		expired, err := accessTokenExpired("2026-08-10T11:59:59Z", now)
		if err != nil || !expired {
			t.Fatalf("expired=%v err=%v, want true nil", expired, err)
		}
	})
	t.Run("success future expires_at", func(t *testing.T) {
		expired, err := accessTokenExpired("2026-08-10T12:00:01Z", now)
		if err != nil || expired {
			t.Fatalf("expired=%v err=%v, want false nil", expired, err)
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

	publicRefresh, _ := json.Marshal(map[string]any{
		"token_endpoint":      server.URL,
		"client_id":           "client",
		"token_endpoint_auth": map[string]any{"type": "none"},
	})
	secretRefresh, _ := json.Marshal(map[string]any{
		"refresh_token":       "old-refresh",
		"token_endpoint_auth": map[string]any{"type": "none"},
	})
	publicAuth := mcpOAuthPublicAuth{
		Type:         "mcp_oauth",
		MCPServerURL: "https://mcp.example.com/mcp",
		ExpiresAt:    "2020-01-01T00:00:00Z",
		Refresh:      publicRefresh,
	}
	secret := mcpOAuthSecretPayload{
		Type:        "mcp_oauth",
		AccessToken: "old-access",
		Refresh:     secretRefresh,
	}
	token, nextAuth, nextSecret, err := exchangeMCPOAuthRefresh(t.Context(), server.Client(), publicAuth, secret, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q", token)
	}
	var savedSecret mcpOAuthSecretPayload
	if err := json.Unmarshal(nextSecret, &savedSecret); err != nil {
		t.Fatalf("unmarshal secret: %v", err)
	}
	var savedRefresh mcpOAuthSecretRefresh
	if err := json.Unmarshal(savedSecret.Refresh, &savedRefresh); err != nil {
		t.Fatalf("unmarshal refresh: %v", err)
	}
	if savedRefresh.RefreshToken != "old-refresh" {
		t.Fatalf("refresh_token = %q, want old-refresh preserved", savedRefresh.RefreshToken)
	}
	var savedAuth mcpOAuthPublicAuth
	if err := json.Unmarshal(nextAuth, &savedAuth); err != nil {
		t.Fatalf("unmarshal auth: %v", err)
	}
	if savedAuth.ExpiresAt == "" {
		t.Fatal("expected expires_at to be set from expires_in")
	}
}

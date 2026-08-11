package vaults

import (
	"testing"
	"time"
)

func TestBuildMCPOAuthStoredCredentialJSON(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	t.Run("failure without refresh omits refresh blocks", func(t *testing.T) {
		publicRaw, secretRaw, err := BuildMCPOAuthStoredCredentialJSON(MCPOAuthStoredCredentialInput{
			MCPServerURL: "https://mcp.example.com/mcp",
			AccessToken:  "access",
			ExpiresIn:    3600,
			Now:          now,
		})
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		publicAuth, err := decodeMCPOAuthCredentialAuth(publicRaw)
		if err != nil {
			t.Fatalf("decode auth: %v", err)
		}
		if publicAuth.Refresh != nil {
			t.Fatalf("refresh = %#v, want nil", publicAuth.Refresh)
		}
		if publicAuth.ExpiresAt == nil || *publicAuth.ExpiresAt != "2026-08-10T13:00:00Z" {
			t.Fatalf("expires_at = %v", publicAuth.ExpiresAt)
		}
		secret, err := decodeMCPOAuthCredentialSecret(secretRaw)
		if err != nil {
			t.Fatalf("decode secret: %v", err)
		}
		if secret.AccessToken != "access" || secret.Refresh != nil {
			t.Fatalf("secret = %#v", secret)
		}
	})

	t.Run("success with refresh uses named schemas", func(t *testing.T) {
		publicRaw, secretRaw, err := BuildMCPOAuthStoredCredentialJSON(MCPOAuthStoredCredentialInput{
			MCPServerURL:            "https://mcp.example.com/mcp",
			AccessToken:             "access",
			RefreshToken:            "refresh",
			TokenEndpoint:           "https://auth.example.com/token",
			ClientID:                "client",
			ClientSecret:            "secret",
			TokenEndpointAuthMethod: "client_secret_post",
			Scope:                   "mcp",
			Resource:                "https://mcp.example.com/mcp",
			Now:                     now,
		})
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		publicAuth, err := decodeMCPOAuthCredentialAuth(publicRaw)
		if err != nil {
			t.Fatalf("decode auth: %v", err)
		}
		if publicAuth.Refresh == nil ||
			publicAuth.Refresh.TokenEndpoint != "https://auth.example.com/token" ||
			publicAuth.Refresh.ClientID != "client" ||
			publicAuth.Refresh.TokenEndpointAuth.Type != "client_secret_post" ||
			publicAuth.Refresh.Scope == nil || *publicAuth.Refresh.Scope != "mcp" ||
			publicAuth.Refresh.Resource == nil || *publicAuth.Refresh.Resource != "https://mcp.example.com/mcp" {
			t.Fatalf("refresh = %#v", publicAuth.Refresh)
		}
		secret, err := decodeMCPOAuthCredentialSecret(secretRaw)
		if err != nil {
			t.Fatalf("decode secret: %v", err)
		}
		if secret.Refresh == nil ||
			secret.Refresh.RefreshToken != "refresh" ||
			secret.Refresh.TokenEndpointAuth == nil ||
			secret.Refresh.TokenEndpointAuth.Type != "client_secret_post" ||
			secret.Refresh.TokenEndpointAuth.ClientSecret != "secret" {
			t.Fatalf("secret refresh = %#v", secret.Refresh)
		}
	})
}

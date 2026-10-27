package vaults

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildMCPOAuthCredentialPayloadsOmitsPlatformClientSecret(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 24, 1, 2, 3, 0, time.UTC)
	base := MCPOAuthCredentialBuildInput{
		MCPServerURL:            "https://api.githubcopilot.com/mcp/",
		ClientID:                "platform-id",
		TokenEndpoint:           "https://github.com/login/oauth/access_token",
		TokenEndpointAuthMethod: "client_secret_post",
		Resource:                "https://api.githubcopilot.com/mcp/",
		FlowScope:               "read:user",
		AccessToken:             "access-token",
		RefreshToken:            "refresh-token",
		TokenScope:              "read:user",
		ExpiresInSeconds:        3600,
		ResolvedClientSecret:    "platform-secret",
		Now:                     now,
	}

	t.Run("platform omits deploy secret from credential", func(t *testing.T) {
		input := base
		input.ClientCredentialSource = MCPOAuthClientCredentialPlatform
		_, secretRaw, err := BuildMCPOAuthCredentialPayloads(input)
		if err != nil {
			t.Fatalf("BuildMCPOAuthCredentialPayloads() error = %v", err)
		}
		assertSecretTokenEndpointAuthClientSecret(t, secretRaw, "")
	})

	t.Run("sealed keeps client secret", func(t *testing.T) {
		input := base
		input.ClientCredentialSource = MCPOAuthClientCredentialSealed
		input.ResolvedClientSecret = "byo-secret"
		_, secretRaw, err := BuildMCPOAuthCredentialPayloads(input)
		if err != nil {
			t.Fatalf("BuildMCPOAuthCredentialPayloads() error = %v", err)
		}
		assertSecretTokenEndpointAuthClientSecret(t, secretRaw, "byo-secret")
	})

	t.Run("typed public auth includes refresh metadata", func(t *testing.T) {
		input := base
		input.ClientCredentialSource = MCPOAuthClientCredentialPlatform
		publicRaw, _, err := BuildMCPOAuthCredentialPayloads(input)
		if err != nil {
			t.Fatalf("BuildMCPOAuthCredentialPayloads() error = %v", err)
		}
		auth, err := decodeCredentialAuth(publicRaw)
		if err != nil {
			t.Fatalf("decodeCredentialAuth() error = %v", err)
		}
		oauth, ok := auth.value.(*mcpOAuthCredentialAuth)
		if !ok {
			t.Fatalf("public auth type = %T, want *mcpOAuthCredentialAuth", auth.value)
		}
		if oauth.ExpiresAt == nil || *oauth.ExpiresAt != "2026-08-24T02:02:03Z" {
			t.Fatalf("expires_at = %v, want 2026-08-24T02:02:03Z", oauth.ExpiresAt)
		}
		if oauth.Refresh == nil || oauth.Refresh.ClientID != "platform-id" {
			t.Fatalf("unexpected refresh: %+v", oauth.Refresh)
		}
		if oauth.Refresh.TokenEndpointAuth.Type != "client_secret_post" {
			t.Fatalf("token_endpoint_auth.type = %q", oauth.Refresh.TokenEndpointAuth.Type)
		}
	})
}

func assertSecretTokenEndpointAuthClientSecret(t *testing.T, secretRaw json.RawMessage, want string) {
	t.Helper()

	secret, err := decodeMCPOAuthCredentialSecret(secretRaw)
	if err != nil {
		t.Fatalf("decodeMCPOAuthCredentialSecret() error = %v", err)
	}
	if secret.Refresh == nil || secret.Refresh.TokenEndpointAuth == nil {
		t.Fatalf("secret refresh.token_endpoint_auth missing: %+v", secret.Refresh)
	}
	got := secret.Refresh.TokenEndpointAuth.ClientSecret
	if got != want {
		t.Fatalf("refresh.token_endpoint_auth.client_secret = %q, want %q", got, want)
	}
}

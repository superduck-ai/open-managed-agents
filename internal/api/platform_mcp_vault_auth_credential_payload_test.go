package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestBuildPlatformMCPVaultOAuthCredentialPayloadsOmitsPlatformClientSecret(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 24, 1, 2, 3, 0, time.UTC)
	baseFlow := db.MCPOAuthFlow{
		MCPServerURL:            "https://api.githubcopilot.com/mcp/",
		TokenEndpoint:           "https://github.com/login/oauth/access_token",
		ClientID:                "platform-id",
		TokenEndpointAuthMethod: "client_secret_post",
		Scope:                   "read:user",
		Resource:                "https://api.githubcopilot.com/mcp/",
	}
	token := platformMCPOAuthTokenResponse{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    float64(3600),
		Scope:        "read:user",
	}

	t.Run("platform omits deploy secret from credential", func(t *testing.T) {
		flow := baseFlow
		flow.ClientCredentialSource = vaultsapi.MCPOAuthClientCredentialPlatform
		_, secretRaw, err := buildPlatformMCPVaultOAuthCredentialPayloads(flow, token, now, "platform-secret")
		if err != nil {
			t.Fatalf("buildPlatformMCPVaultOAuthCredentialPayloads() error = %v", err)
		}
		assertSecretTokenEndpointAuthClientSecret(t, secretRaw, "")
	})

	t.Run("sealed keeps client secret", func(t *testing.T) {
		flow := baseFlow
		flow.ClientCredentialSource = vaultsapi.MCPOAuthClientCredentialSealed
		_, secretRaw, err := buildPlatformMCPVaultOAuthCredentialPayloads(flow, token, now, "byo-secret")
		if err != nil {
			t.Fatalf("buildPlatformMCPVaultOAuthCredentialPayloads() error = %v", err)
		}
		assertSecretTokenEndpointAuthClientSecret(t, secretRaw, "byo-secret")
	})
}

func assertSecretTokenEndpointAuthClientSecret(t *testing.T, secretRaw json.RawMessage, want string) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(secretRaw, &payload); err != nil {
		t.Fatalf("unmarshal secret payload: %v", err)
	}
	refresh, ok := payload["refresh"].(map[string]any)
	if !ok {
		t.Fatalf("secret refresh missing: %#v", payload)
	}
	auth, ok := refresh["token_endpoint_auth"].(map[string]any)
	if !ok {
		t.Fatalf("token_endpoint_auth missing: %#v", refresh)
	}
	got, _ := auth["client_secret"].(string)
	if got != want {
		t.Fatalf("refresh.token_endpoint_auth.client_secret = %q, want %q", got, want)
	}
	if _, has := auth["client_secret"]; want == "" && has {
		t.Fatalf("platform credential must omit client_secret key, got %#v", auth)
	}
}

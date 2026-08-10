package vaults

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestNormalizeCredentialAuthForUpdateRejectsWrongSecretSchema(t *testing.T) {
	t.Parallel()

	_, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "static_bearer",
		CredentialKey: "https://mcp.example.com",
		Auth:          json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://mcp.example.com"}`),
	}, []byte(`{"type":"environment_variable","secret_value":"value"}`), json.RawMessage(`{"type":"static_bearer","token":"new-token"}`))
	if err == nil || !strings.Contains(err.Error(), "static_bearer secret has type") {
		t.Fatalf("normalize update error = %v", err)
	}
}

func TestNormalizeCredentialAuthForUpdatePreservesMCPOAuthRefresh(t *testing.T) {
	t.Parallel()

	state, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "mcp_oauth",
		CredentialKey: "https://mcp.example.com",
		Auth: json.RawMessage(`{
			"type":"mcp_oauth",
			"mcp_server_url":"https://mcp.example.com",
			"refresh":{
				"token_endpoint":"https://auth.example.com/token",
				"client_id":"client-id",
				"token_endpoint_auth":{"type":"client_secret_basic"}
			}
		}`),
	}, []byte(`{
			"type":"mcp_oauth",
			"access_token":"old-access-token",
			"refresh":{
				"refresh_token":"refresh-token",
				"token_endpoint_auth":{"type":"client_secret_basic","client_secret":"client-secret"}
			}
		}`), json.RawMessage(`{"type":"mcp_oauth","access_token":"new-access-token"}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}

	secret, err := decodeMCPOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode normalized secret: %v", err)
	}
	if secret.AccessToken != "new-access-token" || secret.Refresh == nil || secret.Refresh.RefreshToken != "refresh-token" {
		t.Fatalf("unexpected normalized secret: %+v", secret)
	}
	if secret.Refresh.TokenEndpointAuth == nil || secret.Refresh.TokenEndpointAuth.ClientSecret != "client-secret" {
		t.Fatalf("unexpected token endpoint auth secret: %+v", secret.Refresh.TokenEndpointAuth)
	}
}

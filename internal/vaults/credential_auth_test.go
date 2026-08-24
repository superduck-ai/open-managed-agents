package vaults

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestNormalizeCredentialAuthForUpdateRequiresCompleteReplacementWithoutSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current db.VaultCredential
		update  string
	}{
		{
			name: "static bearer token",
			current: db.VaultCredential{
				AuthType:      "static_bearer",
				CredentialKey: "https://mcp.example.com",
				Auth:          json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://mcp.example.com"}`),
			},
			update: `{"type":"static_bearer"}`,
		},
		{
			name: "environment variable value",
			current: db.VaultCredential{
				AuthType:      "environment_variable",
				CredentialKey: "TOKEN",
				Auth: json.RawMessage(`{
					"type":"environment_variable",
					"secret_name":"TOKEN",
					"placeholder":"oma_ph_testplaceholder0123456789abcd",
					"networking":{"type":"unrestricted"},
					"injection_location":{"header":true,"body":false}
				}`),
			},
			update: `{"type":"environment_variable"}`,
		},
		{
			name: "oauth refresh secret",
			current: db.VaultCredential{
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
			},
			update: `{"type":"mcp_oauth","access_token":"replacement-token"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := normalizeCredentialAuthForUpdate(test.current, nil, json.RawMessage(test.update))
			if !errors.Is(err, ErrMissingSecretEnvelope) {
				t.Fatalf("normalize update error = %v, want ErrMissingSecretEnvelope", err)
			}
		})
	}
}

func TestNormalizeCredentialAuthForCreateRejectsSchemaMismatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "string field",
			raw:  `{"type":"static_bearer","mcp_server_url":42,"token":"token"}`,
		},
		{
			name: "nested object",
			raw:  `{"type":"mcp_oauth","mcp_server_url":"https://mcp.example.com","access_token":"token","refresh":"invalid"}`,
		},
		{
			name: "networking discriminator",
			raw:  `{"type":"environment_variable","secret_name":"TOKEN","secret_value":"secret","networking":{"type":42}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := normalizeCredentialAuthForCreate(json.RawMessage(test.raw))
			if err == nil {
				t.Fatal("normalize create succeeded")
			}
		})
	}
}

func TestNormalizeCredentialAuthForUpdateRejectsInvalidStoredPublicSchema(t *testing.T) {
	t.Parallel()

	_, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "static_bearer",
		CredentialKey: "https://mcp.example.com",
		Auth:          json.RawMessage(`{"type":"static_bearer","mcp_server_url":42}`),
	}, []byte(`{"type":"static_bearer","token":"old-token"}`), json.RawMessage(`{"type":"static_bearer","token":"new-token"}`))
	if err == nil || !strings.Contains(err.Error(), "decode stored credential auth") {
		t.Fatalf("normalize update error = %v", err)
	}
}

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

func TestNormalizeCredentialAuthForUpdateReplacesMissingOAuthSecret(t *testing.T) {
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
	}, nil, json.RawMessage(`{"type":"mcp_oauth","access_token":"replacement-token","refresh":null}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	secret, err := decodeMCPOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if secret.AccessToken != "replacement-token" || secret.Refresh != nil {
		t.Fatalf("unexpected replacement secret: %+v", secret)
	}
}

func TestNormalizeCredentialAuthForUpdateAppliesTypedNullPatch(t *testing.T) {
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
				"token_endpoint_auth":{"type":"client_secret_basic"},
				"scope":"read"
			}
		}`),
	}, []byte(`{
		"type":"mcp_oauth",
		"access_token":"access-token",
		"refresh":{
			"refresh_token":"refresh-token",
			"token_endpoint_auth":{"type":"client_secret_basic","client_secret":"client-secret"}
		}
	}`), json.RawMessage(`{
		"type":"mcp_oauth",
		"refresh":{"scope":null,"token_endpoint_auth":null}
	}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}

	public, err := decodeCredentialAuth(state.PublicAuth)
	if err != nil {
		t.Fatalf("decode public auth: %v", err)
	}
	oauth, ok := public.value.(*mcpOAuthCredentialAuth)
	if !ok || oauth.Refresh == nil {
		t.Fatalf("unexpected public auth: %#v", public.value)
	}
	if oauth.Refresh.Scope != nil || oauth.Refresh.TokenEndpointAuth.Type != "none" {
		t.Fatalf("unexpected public refresh: %+v", oauth.Refresh)
	}
	secret, err := decodeMCPOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if secret.Refresh == nil || secret.Refresh.TokenEndpointAuth == nil || secret.Refresh.TokenEndpointAuth.Type != "none" {
		t.Fatalf("unexpected secret refresh: %+v", secret.Refresh)
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

func TestNormalizeEnvironmentVariablePreservesSecretValueWhitespace(t *testing.T) {
	t.Parallel()

	const createValue = "  line-one\nline-two\n"
	createState, err := normalizeCredentialAuthForCreate(json.RawMessage(`{
		"type":"environment_variable",
		"secret_name":" API_KEY ",
		"secret_value":"  line-one\nline-two\n",
		"networking":{"type":"unrestricted"}
	}`))
	if err != nil {
		t.Fatalf("normalize create: %v", err)
	}
	createSecret, err := decodeEnvironmentVariableCredentialSecret(createState.SecretPayload)
	if err != nil {
		t.Fatalf("decode create secret: %v", err)
	}
	if createSecret.SecretValue != createValue {
		t.Fatalf("create secret_value = %q, want verbatim %q", createSecret.SecretValue, createValue)
	}
	if createState.Key != "API_KEY" {
		t.Fatalf("create credential key = %q, want trimmed secret_name", createState.Key)
	}

	const updateValue = "  rotated\n"
	updateState, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "environment_variable",
		CredentialKey: "API_KEY",
		Auth: json.RawMessage(`{
			"type":"environment_variable",
			"secret_name":"API_KEY",
			"networking":{"type":"unrestricted"},
			"placeholder":"oma_ph_test",
			"injection_location":{"header":true,"body":false}
		}`),
	}, []byte(`{"type":"environment_variable","secret_value":"old"}`), json.RawMessage(`{
		"type":"environment_variable",
		"secret_value":"  rotated\n"
	}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	updateSecret, err := decodeEnvironmentVariableCredentialSecret(updateState.SecretPayload)
	if err != nil {
		t.Fatalf("decode update secret: %v", err)
	}
	if updateSecret.SecretValue != updateValue {
		t.Fatalf("update secret_value = %q, want verbatim %q", updateSecret.SecretValue, updateValue)
	}
}

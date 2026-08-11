package vaults

import (
	"testing"
)

func TestDecodeCredentialSecretFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		decode func([]byte) error
		raw    string
	}{
		{
			name: "invalid concrete field",
			decode: func(raw []byte) error {
				_, err := decodeStaticBearerCredentialSecret(raw)
				return err
			},
			raw: `{"type":"static_bearer","token":42}`,
		},
		{
			name: "wrong discriminator",
			decode: func(raw []byte) error {
				_, err := decodeMCPOAuthCredentialSecret(raw)
				return err
			},
			raw: `{"type":"environment_variable","access_token":"token"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.decode([]byte(test.raw)); err == nil {
				t.Fatal("decode secret succeeded, want error")
			}
		})
	}
}

func TestDecodeCredentialSecrets(t *testing.T) {
	t.Parallel()

	oauth, err := decodeMCPOAuthCredentialSecret([]byte(`{
		"type":"mcp_oauth",
		"access_token":"access-token",
		"refresh":{
			"refresh_token":"refresh-token",
			"token_endpoint_auth":{"type":"client_secret_basic","client_secret":"client-secret"}
		}
	}`))
	if err != nil {
		t.Fatalf("decode OAuth secret: %v", err)
	}
	if oauth.Refresh == nil || oauth.Refresh.RefreshToken != "refresh-token" || oauth.Refresh.TokenEndpointAuth == nil {
		t.Fatalf("unexpected OAuth secret: %+v", oauth)
	}

	bearer, err := decodeStaticBearerCredentialSecret([]byte(`{"type":"static_bearer","token":"token"}`))
	if err != nil {
		t.Fatalf("decode static bearer secret: %v", err)
	}
	if bearer.Token != "token" {
		t.Fatalf("token = %q", bearer.Token)
	}

	environment, err := decodeEnvironmentVariableCredentialSecret([]byte(`{"type":"environment_variable","secret_value":"value"}`))
	if err != nil {
		t.Fatalf("decode environment variable secret: %v", err)
	}
	if environment.SecretValue != "value" {
		t.Fatalf("secret_value = %q", environment.SecretValue)
	}
}

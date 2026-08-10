package vaults

import (
	"encoding/json"
	"fmt"
)

type credentialSecretVariant interface {
	credentialSecretVariant()
}

type mcpOAuthCredentialSecret struct {
	Type        credentialAuthType     `json:"type"`
	AccessToken string                 `json:"access_token"`
	Refresh     *mcpOAuthRefreshSecret `json:"refresh,omitempty"`
}

func (mcpOAuthCredentialSecret) credentialSecretVariant() {}

type mcpOAuthRefreshSecret struct {
	RefreshToken      string                   `json:"refresh_token"`
	TokenEndpointAuth *tokenEndpointAuthSecret `json:"token_endpoint_auth,omitempty"`
}

type tokenEndpointAuthSecret struct {
	Type         string `json:"type"`
	ClientSecret string `json:"client_secret,omitempty"`
}

type staticBearerCredentialSecret struct {
	Type  credentialAuthType `json:"type"`
	Token string             `json:"token"`
}

func (staticBearerCredentialSecret) credentialSecretVariant() {}

type environmentVariableCredentialSecret struct {
	Type        credentialAuthType `json:"type"`
	SecretValue string             `json:"secret_value"`
}

func (environmentVariableCredentialSecret) credentialSecretVariant() {}

func decodeMCPOAuthCredentialSecret(raw []byte) (mcpOAuthCredentialSecret, error) {
	var secret mcpOAuthCredentialSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return mcpOAuthCredentialSecret{}, fmt.Errorf("decode mcp_oauth secret: %w", err)
	}
	if secret.Type != credentialAuthTypeMCPOAuth {
		return mcpOAuthCredentialSecret{}, fmt.Errorf("mcp_oauth secret has type %q", secret.Type)
	}
	return secret, nil
}

func decodeStaticBearerCredentialSecret(raw []byte) (staticBearerCredentialSecret, error) {
	var secret staticBearerCredentialSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return staticBearerCredentialSecret{}, fmt.Errorf("decode static_bearer secret: %w", err)
	}
	if secret.Type != credentialAuthTypeStaticBearer {
		return staticBearerCredentialSecret{}, fmt.Errorf("static_bearer secret has type %q", secret.Type)
	}
	return secret, nil
}

func decodeEnvironmentVariableCredentialSecret(raw []byte) (environmentVariableCredentialSecret, error) {
	var secret environmentVariableCredentialSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return environmentVariableCredentialSecret{}, fmt.Errorf("decode environment_variable secret: %w", err)
	}
	if secret.Type != credentialAuthTypeEnvironmentVariable {
		return environmentVariableCredentialSecret{}, fmt.Errorf("environment_variable secret has type %q", secret.Type)
	}
	return secret, nil
}

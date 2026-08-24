package vaults

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type credentialAuthType string

const (
	credentialAuthTypeMCPOAuth            credentialAuthType = "mcp_oauth"
	credentialAuthTypeStaticBearer        credentialAuthType = "static_bearer"
	credentialAuthTypeEnvironmentVariable credentialAuthType = "environment_variable"
)

// credentialAuth is the public, non-secret auth configuration stored in the
// database. UnmarshalJSON uses auth.type as a discriminator and materializes
// one of the concrete schemas below.
type credentialAuth struct {
	value credentialAuthVariant
}

type credentialAuthVariant interface {
	credentialAuthVariant()
}

type mcpOAuthCredentialAuth struct {
	Type                   credentialAuthType `json:"type"`
	MCPServerURL           string             `json:"mcp_server_url"`
	ClientCredentialSource string             `json:"client_credential_source,omitempty"`
	ExpiresAt              *string            `json:"expires_at,omitempty"`
	Refresh                *mcpOAuthRefresh   `json:"refresh,omitempty"`
}

func (*mcpOAuthCredentialAuth) credentialAuthVariant() {}

type mcpOAuthRefresh struct {
	TokenEndpoint     string            `json:"token_endpoint"`
	ClientID          string            `json:"client_id"`
	TokenEndpointAuth tokenEndpointAuth `json:"token_endpoint_auth"`
	Scope             *string           `json:"scope,omitempty"`
	Resource          *string           `json:"resource,omitempty"`
}

type tokenEndpointAuth struct {
	Type string `json:"type"`
}

type staticBearerCredentialAuth struct {
	Type         credentialAuthType `json:"type"`
	MCPServerURL string             `json:"mcp_server_url"`
}

func (*staticBearerCredentialAuth) credentialAuthVariant() {}

type environmentVariableCredentialAuth struct {
	Type       credentialAuthType       `json:"type"`
	SecretName string                   `json:"secret_name"`
	Networking credentialAuthNetworking `json:"networking"`
}

func (*environmentVariableCredentialAuth) credentialAuthVariant() {}

type credentialAuthNetworking struct {
	Type         string    `json:"type"`
	AllowedHosts *[]string `json:"allowed_hosts,omitempty"`
}

func (a *credentialAuth) UnmarshalJSON(data []byte) error {
	var discriminator struct {
		Type credentialAuthType `json:"type"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return fmt.Errorf("decode auth type: %w", err)
	}

	var value credentialAuthVariant
	switch discriminator.Type {
	case credentialAuthTypeMCPOAuth:
		value = &mcpOAuthCredentialAuth{}
	case credentialAuthTypeStaticBearer:
		value = &staticBearerCredentialAuth{}
	case credentialAuthTypeEnvironmentVariable:
		value = &environmentVariableCredentialAuth{}
	case "":
		return errors.New("auth.type is required")
	default:
		return fmt.Errorf("unsupported auth.type %q", discriminator.Type)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode %s auth: %w", discriminator.Type, err)
	}
	a.value = value
	return nil
}

func (a credentialAuth) MarshalJSON() ([]byte, error) {
	if a.value == nil {
		return nil, errors.New("credential auth is empty")
	}
	return json.Marshal(a.value)
}

func decodeCredentialAuth(raw []byte) (credentialAuth, error) {
	var auth credentialAuth
	if err := json.Unmarshal(raw, &auth); err != nil {
		return credentialAuth{}, err
	}
	return auth, nil
}

func decodeMCPOAuthCredentialAuth(raw []byte) (*mcpOAuthCredentialAuth, error) {
	if len(raw) == 0 {
		return nil, emptyMCPOAuthAuth()
	}
	auth, err := decodeCredentialAuth(raw)
	if err != nil {
		return nil, err
	}
	value, ok := auth.value.(*mcpOAuthCredentialAuth)
	if !ok || value == nil {
		return nil, credentialAuthNotMCPOAuth()
	}
	if strings.TrimSpace(value.MCPServerURL) == "" {
		return nil, mcpOAuthServerURLRequired()
	}
	return value, nil
}

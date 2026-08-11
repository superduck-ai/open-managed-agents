package vaults

import (
	"encoding/json"
	"strings"
	"time"
)

// MCPOAuthStoredCredentialInput is the seam for materializing mcp_oauth public
// auth + secret JSON after a successful token exchange (e.g. platform OAuth
// callback). Callers pass primitives; vaults owns the named storage schemas.
type MCPOAuthStoredCredentialInput struct {
	MCPServerURL            string
	AccessToken             string
	RefreshToken            string
	TokenEndpoint           string
	ClientID                string
	ClientSecret            string
	TokenEndpointAuthMethod string
	Scope                   string
	Resource                string
	ExpiresIn               int64 // seconds; <=0 omits expires_at
	Now                     time.Time
}

// BuildMCPOAuthStoredCredentialJSON encodes mcp_oauth public auth and secret
// payloads using the same named schemas as Vault credential create/refresh.
func BuildMCPOAuthStoredCredentialJSON(in MCPOAuthStoredCredentialInput) (json.RawMessage, json.RawMessage, error) {
	publicAuth := mcpOAuthCredentialAuth{
		Type:         credentialAuthTypeMCPOAuth,
		MCPServerURL: in.MCPServerURL,
	}
	if in.ExpiresIn > 0 {
		expiresAt := in.Now.UTC().Add(time.Duration(in.ExpiresIn) * time.Second).Format(time.RFC3339)
		publicAuth.ExpiresAt = &expiresAt
	}

	secretPayload := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: in.AccessToken,
	}

	if refreshToken := strings.TrimSpace(in.RefreshToken); refreshToken != "" {
		method := strings.TrimSpace(in.TokenEndpointAuthMethod)
		if method == "" {
			method = "none"
		}
		publicRefresh := mcpOAuthRefresh{
			TokenEndpoint:     in.TokenEndpoint,
			ClientID:          in.ClientID,
			TokenEndpointAuth: tokenEndpointAuth{Type: method},
		}
		if scope := strings.TrimSpace(in.Scope); scope != "" {
			publicRefresh.Scope = &scope
		}
		if resource := strings.TrimSpace(in.Resource); resource != "" {
			publicRefresh.Resource = &resource
		}
		publicAuth.Refresh = &publicRefresh

		secretAuth := &tokenEndpointAuthSecret{Type: method}
		if (method == "client_secret_basic" || method == "client_secret_post") && strings.TrimSpace(in.ClientSecret) != "" {
			secretAuth.ClientSecret = in.ClientSecret
		}
		secretPayload.Refresh = &mcpOAuthRefreshSecret{
			RefreshToken:      refreshToken,
			TokenEndpointAuth: secretAuth,
		}
	}

	publicJSON, err := json.Marshal(publicAuth)
	if err != nil {
		return nil, nil, err
	}
	secretJSON, err := json.Marshal(secretPayload)
	if err != nil {
		return nil, nil, err
	}
	return json.RawMessage(publicJSON), json.RawMessage(secretJSON), nil
}

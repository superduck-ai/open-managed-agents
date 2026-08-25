package vaults

import (
	"encoding/json"
	"time"
)

// MCPOAuthStoredCredentialInput is the seam for materializing mcp_oauth public
// auth + secret JSON after a successful token exchange (e.g. platform OAuth
// callback). Callers pass primitives; vaults owns the named storage schemas.
//
// ClientSecret may be the deploy-config platform secret used for the exchange;
// ClientCredentialSource decides whether it is copied into the sealed refresh
// payload (sealed only) and is stored on public auth so refresh can re-resolve.
type MCPOAuthStoredCredentialInput struct {
	MCPServerURL            string
	AccessToken             string
	RefreshToken            string
	TokenEndpoint           string
	ClientID                string
	ClientSecret            string
	ClientCredentialSource  string
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
	if in.ClientCredentialSource == MCPOAuthClientCredentialPlatform ||
		in.ClientCredentialSource == MCPOAuthClientCredentialSealed {
		publicAuth.ClientCredentialSource = in.ClientCredentialSource
	}
	if in.ExpiresIn > 0 {
		expiresAt := in.Now.UTC().Add(time.Duration(in.ExpiresIn) * time.Second).Format(time.RFC3339)
		publicAuth.ExpiresAt = &expiresAt
	}

	secretPayload := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: in.AccessToken,
	}

	if in.RefreshToken != "" {
		method := in.TokenEndpointAuthMethod
		if method == "" {
			method = "none"
		}
		persistSecret := ClientSecretForMCPOAuthPersist(in.ClientCredentialSource, in.ClientSecret)
		publicRefresh := mcpOAuthRefresh{
			TokenEndpoint:     in.TokenEndpoint,
			ClientID:          in.ClientID,
			TokenEndpointAuth: tokenEndpointAuth{Type: method},
		}
		if in.Scope != "" {
			scope := in.Scope
			publicRefresh.Scope = &scope
		}
		if in.Resource != "" {
			resource := in.Resource
			publicRefresh.Resource = &resource
		}
		publicAuth.Refresh = &publicRefresh

		secretAuth := &tokenEndpointAuthSecret{Type: method}
		if (method == "client_secret_basic" || method == "client_secret_post") && persistSecret != "" {
			secretAuth.ClientSecret = persistSecret
		}
		secretPayload.Refresh = &mcpOAuthRefreshSecret{
			RefreshToken:      in.RefreshToken,
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

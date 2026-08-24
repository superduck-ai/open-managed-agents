package vaults

import (
	"encoding/json"
	"time"
)

// MCPOAuthCredentialBuildInput is the post-token-exchange surface used to
// materialize vault credential public auth and sealed secret payloads.
//
// ResolvedClientSecret may be the deploy-config platform secret used for the
// exchange; ClientCredentialSource decides whether it is copied into the
// sealed refresh payload (sealed only).
type MCPOAuthCredentialBuildInput struct {
	MCPServerURL            string
	ClientID                string
	ClientCredentialSource  string
	TokenEndpoint           string
	TokenEndpointAuthMethod string
	Resource                string
	FlowScope               string
	AccessToken             string
	RefreshToken            string
	TokenScope              string
	ExpiresInSeconds        int64
	ResolvedClientSecret    string
	Now                     time.Time
}

// BuildMCPOAuthCredentialPayloads builds typed mcp_oauth public auth and secret
// payloads for vault_credentials after a successful OAuth token exchange.
func BuildMCPOAuthCredentialPayloads(input MCPOAuthCredentialBuildInput) (publicAuth, secretPayload json.RawMessage, err error) {
	public := &mcpOAuthCredentialAuth{
		Type:         credentialAuthTypeMCPOAuth,
		MCPServerURL: input.MCPServerURL,
	}
	if input.ExpiresInSeconds > 0 {
		expiresAt := input.Now.Add(time.Duration(input.ExpiresInSeconds) * time.Second).UTC().Format(time.RFC3339)
		public.ExpiresAt = &expiresAt
	}
	secret := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: input.AccessToken,
	}
	if input.RefreshToken != "" {
		persistSecret := ClientSecretForMCPOAuthPersist(input.ClientCredentialSource, input.ResolvedClientSecret)
		publicTokenAuth, secretTokenAuth := mcpOAuthTokenEndpointAuthForPersist(input.TokenEndpointAuthMethod, persistSecret)
		publicRefresh := mcpOAuthRefresh{
			TokenEndpoint:     input.TokenEndpoint,
			ClientID:          input.ClientID,
			TokenEndpointAuth: publicTokenAuth,
		}
		if scope := firstNonEmptyString(input.TokenScope, input.FlowScope); scope != "" {
			publicRefresh.Scope = &scope
		}
		if input.Resource != "" {
			resource := input.Resource
			publicRefresh.Resource = &resource
		}
		public.Refresh = &publicRefresh
		secret.Refresh = &mcpOAuthRefreshSecret{
			RefreshToken:      input.RefreshToken,
			TokenEndpointAuth: &secretTokenAuth,
		}
	}
	state, err := credentialAuthStateFromValues(credentialAuthTypeMCPOAuth, input.MCPServerURL, public, secret)
	if err != nil {
		return nil, nil, err
	}
	return state.PublicAuth, state.SecretPayload, nil
}

// mcpOAuthTokenEndpointAuthForPersist builds public/secret token_endpoint_auth
// for persistence. Empty persistClientSecret omits client_secret (platform).
func mcpOAuthTokenEndpointAuthForPersist(method, persistClientSecret string) (tokenEndpointAuth, tokenEndpointAuthSecret) {
	if method == "" {
		method = "none"
	}
	public := tokenEndpointAuth{Type: method}
	secret := tokenEndpointAuthSecret{Type: method}
	if (method == "client_secret_basic" || method == "client_secret_post") && persistClientSecret != "" {
		secret.ClientSecret = persistClientSecret
	}
	return public, secret
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

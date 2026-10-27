package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

// MCP OAuth flow client credential sources. Platform secrets stay in deploy
// config and are re-resolved at token exchange; sealed covers BYO and DCR.
const (
	MCPOAuthClientCredentialPlatform = "platform"
	MCPOAuthClientCredentialSealed   = "sealed"
)

var (
	errMCPOAuthFlowSecretServiceRequired = errors.New("mcp oauth flow secret service is required")
	errMCPOAuthFlowCodeVerifierRequired  = errors.New("mcp oauth flow code_verifier is required")
	errMCPOAuthFlowEnvelopeRequired      = errors.New("mcp oauth flow secret envelope is required")
	errMCPOAuthPlatformClientMissing     = errors.New("platform oauth client secret is unavailable for mcp server")
)

type mcpOAuthFlowSecretPayload struct {
	ClientSecret string `json:"client_secret,omitempty"`
	CodeVerifier string `json:"code_verifier"`
}

// mcpOAuthFlowSecretBinding binds the envelope to the pending flow identity.
// CredentialExternalID carries the flow external_id so a stolen row cannot be
// opened under another flow.
func mcpOAuthFlowSecretBinding(flow db.MCPOAuthFlow) secrets.Binding {
	return secrets.Binding{
		OrganizationUUID:     flow.OrganizationUUID,
		WorkspaceUUID:        flow.WorkspaceUUID,
		VaultExternalID:      flow.VaultExternalID,
		CredentialExternalID: flow.ExternalID,
	}
}

// ClientSecretForMCPOAuthPersist returns the client_secret that may enter a
// user-owned seal (pending mcp_oauth_flows or vault_credentials). Only sealed
// (BYO / DCR) secrets persist; platform and any other source yield empty so
// deploy-config secrets stay in vault.platform_oauth_clients.
//
// source must already be an exact MCPOAuthClientCredential* value (API/DB
// seam); this helper does not normalize whitespace.
func ClientSecretForMCPOAuthPersist(source, clientSecret string) string {
	if source == MCPOAuthClientCredentialSealed {
		return clientSecret
	}
	return ""
}

// SealMCPOAuthFlowSecrets seals code_verifier and any flow-owned client_secret.
// Platform secrets are dropped using flow.ClientCredentialSource; callers may
// pass the exchange-time resolved secret.
// codeVerifier is an internal PKCE value and must be non-empty as generated.
func SealMCPOAuthFlowSecrets(
	ctx context.Context,
	secretSvc *secrets.Service,
	flow db.MCPOAuthFlow,
	clientSecret, codeVerifier string,
) (secrets.Envelope, error) {
	if secretSvc == nil {
		return secrets.Envelope{}, errMCPOAuthFlowSecretServiceRequired
	}
	if codeVerifier == "" {
		return secrets.Envelope{}, errMCPOAuthFlowCodeVerifierRequired
	}
	payload, err := json.Marshal(mcpOAuthFlowSecretPayload{
		ClientSecret: ClientSecretForMCPOAuthPersist(flow.ClientCredentialSource, clientSecret),
		CodeVerifier: codeVerifier,
	})
	if err != nil {
		return secrets.Envelope{}, err
	}
	envelope, err := secretSvc.Seal(ctx, mcpOAuthFlowSecretBinding(flow), payload)
	if err != nil {
		return secrets.Envelope{}, fmt.Errorf("seal mcp oauth flow secrets: %w", err)
	}
	return envelope, nil
}

// OpenMCPOAuthFlowSecrets decrypts a flow envelope into transient plaintext.
// Callers must not persist or log the returned values.
func OpenMCPOAuthFlowSecrets(
	ctx context.Context,
	secretSvc *secrets.Service,
	flow db.MCPOAuthFlow,
) (clientSecret, codeVerifier string, err error) {
	if secretSvc == nil {
		return "", "", errMCPOAuthFlowSecretServiceRequired
	}
	if flow.SecretEnvelope == nil {
		return "", "", errMCPOAuthFlowEnvelopeRequired
	}
	plaintext, err := secretSvc.Open(ctx, mcpOAuthFlowSecretBinding(flow), *flow.SecretEnvelope)
	if err != nil {
		return "", "", fmt.Errorf("open mcp oauth flow secrets: %w", err)
	}
	defer clear(plaintext)

	var payload mcpOAuthFlowSecretPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return "", "", fmt.Errorf("decode mcp oauth flow secrets: %w", err)
	}
	if payload.CodeVerifier == "" {
		return "", "", errMCPOAuthFlowCodeVerifierRequired
	}
	return payload.ClientSecret, payload.CodeVerifier, nil
}

// ResolveMCPOAuthTokenClientSecret returns the client_secret used for token
// exchange and refresh. Platform credentials re-read deploy config; sealed
// credentials use the opened envelope value (BYO or DCR).
//
// source must already be an exact MCPOAuthClientCredential* value (or empty for
// legacy sealed). Config secret trimming happens in FindPlatformOAuthClient.
func ResolveMCPOAuthTokenClientSecret(
	source, mcpServerURL, openedClientSecret string,
	clients []config.PlatformOAuthClientConfig,
) (string, error) {
	switch source {
	case MCPOAuthClientCredentialPlatform:
		entry, ok := config.FindPlatformOAuthClient(clients, mcpServerURL)
		if !ok {
			return "", errMCPOAuthPlatformClientMissing
		}
		return entry.ClientSecret, nil
	case MCPOAuthClientCredentialSealed, "":
		return openedClientSecret, nil
	default:
		return "", fmt.Errorf("unknown mcp oauth client credential source %q", source)
	}
}

// mcpOAuthRefreshClientCredentialSource is the source Resolve sees at refresh.
// Stored public auth wins; otherwise infer so credentials created before the
// field existed still refresh: envelope secret → sealed; confidential method
// without a persisted secret → platform.
func mcpOAuthRefreshClientCredentialSource(auth *mcpOAuthCredentialAuth, secret mcpOAuthCredentialSecret) string {
	if auth != nil && auth.ClientCredentialSource != "" {
		return auth.ClientCredentialSource
	}
	method := "none"
	opened := ""
	if secret.Refresh != nil && secret.Refresh.TokenEndpointAuth != nil {
		method = secret.Refresh.TokenEndpointAuth.Type
		opened = secret.Refresh.TokenEndpointAuth.ClientSecret
	}
	if opened != "" {
		return MCPOAuthClientCredentialSealed
	}
	if method == "client_secret_basic" || method == "client_secret_post" {
		return MCPOAuthClientCredentialPlatform
	}
	return MCPOAuthClientCredentialSealed
}

func resolveMCPOAuthRefreshClientSecret(
	auth *mcpOAuthCredentialAuth,
	secret mcpOAuthCredentialSecret,
	clients []config.PlatformOAuthClientConfig,
) (string, error) {
	opened := ""
	if secret.Refresh != nil && secret.Refresh.TokenEndpointAuth != nil {
		opened = secret.Refresh.TokenEndpointAuth.ClientSecret
	}
	mcpServerURL := ""
	if auth != nil {
		mcpServerURL = auth.MCPServerURL
	}
	return ResolveMCPOAuthTokenClientSecret(
		mcpOAuthRefreshClientCredentialSource(auth, secret),
		mcpServerURL,
		opened,
		clients,
	)
}

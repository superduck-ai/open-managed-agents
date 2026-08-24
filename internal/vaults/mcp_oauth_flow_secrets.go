package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

// ClientSecretForMCPOAuthPersist drops platform secrets before any user-owned
// seal (pending mcp_oauth_flows or vault_credentials). Deploy-config secrets
// stay in vault.platform_oauth_clients and are re-resolved at token exchange /
// future refresh; sealed covers BYO and DCR only.
func ClientSecretForMCPOAuthPersist(source, clientSecret string) string {
	if strings.TrimSpace(source) == MCPOAuthClientCredentialPlatform {
		return ""
	}
	return clientSecret
}

// SealMCPOAuthFlowSecrets seals code_verifier and any flow-owned client_secret.
// Platform flows must pass an empty client_secret (use ClientSecretForMCPOAuthPersist).
func SealMCPOAuthFlowSecrets(
	ctx context.Context,
	secretSvc *secrets.Service,
	flow db.MCPOAuthFlow,
	clientSecret, codeVerifier string,
) (secrets.Envelope, error) {
	if secretSvc == nil {
		return secrets.Envelope{}, errMCPOAuthFlowSecretServiceRequired
	}
	codeVerifier = strings.TrimSpace(codeVerifier)
	if codeVerifier == "" {
		return secrets.Envelope{}, errMCPOAuthFlowCodeVerifierRequired
	}
	payload, err := json.Marshal(mcpOAuthFlowSecretPayload{
		ClientSecret: clientSecret,
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
	if strings.TrimSpace(payload.CodeVerifier) == "" {
		return "", "", errMCPOAuthFlowCodeVerifierRequired
	}
	return payload.ClientSecret, payload.CodeVerifier, nil
}

// ResolveMCPOAuthTokenClientSecret returns the client_secret used for token
// exchange. Platform flows re-read deploy config; sealed flows use the opened
// envelope value (BYO or DCR).
func ResolveMCPOAuthTokenClientSecret(
	source, mcpServerURL, openedClientSecret string,
	clients []config.PlatformOAuthClientConfig,
) (string, error) {
	switch strings.TrimSpace(source) {
	case MCPOAuthClientCredentialPlatform:
		entry, ok := config.FindPlatformOAuthClient(clients, mcpServerURL)
		if !ok {
			return "", errMCPOAuthPlatformClientMissing
		}
		return strings.TrimSpace(entry.ClientSecret), nil
	case MCPOAuthClientCredentialSealed, "":
		return openedClientSecret, nil
	default:
		return "", fmt.Errorf("unknown mcp oauth client credential source %q", source)
	}
}

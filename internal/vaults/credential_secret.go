package vaults

import (
	"context"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

// ErrMissingSecretEnvelope is returned when an active credential has no
// secret envelope to open. The Vaults API maps this to HTTP 400 so clients can
// resubmit the secret after a direct-cutover discard; the MCP proxy maps it to
// HTTP 502 because the upstream credential is unavailable.
var ErrMissingSecretEnvelope = errors.New("vault credential secret is missing; resubmit the secret")

// credentialBinding builds the AAD binding from a credential's identity. The
// same binding is used to seal and open, so an envelope cannot be moved to
// another org/workspace/vault/credential and still decrypt.
func credentialBinding(credential db.VaultCredential) secrets.Binding {
	return secrets.Binding{
		OrganizationUUID:     credential.OrganizationUUID,
		WorkspaceUUID:        credential.WorkspaceUUID,
		VaultExternalID:      credential.VaultExternalID,
		CredentialExternalID: credential.ExternalID,
	}
}

// SealCredentialSecret seals a credential's plaintext SecretPayload into
// SecretEnvelope and drops the plaintext so it is never persisted. Empty or
// JSON-null payloads are rejected so create/update callers cannot believe a
// seal succeeded when no envelope was produced. Exported so the platform MCP
// OAuth callback can seal credentials through the same path as the Vaults API.
func SealCredentialSecret(ctx context.Context, secretSvc *secrets.Service, credential *db.VaultCredential) error {
	if len(credential.SecretPayload) == 0 || isJSONNull(credential.SecretPayload) {
		return errors.New("vault credential secret payload is required to seal")
	}
	envelope, err := secretSvc.Seal(ctx, credentialBinding(*credential), credential.SecretPayload)
	if err != nil {
		return fmt.Errorf("seal credential secret: %w", err)
	}
	credential.SecretEnvelope = &envelope
	clearCredentialSecretPayload(credential)
	return nil
}

// openCredentialSecret decrypts a credential's envelope and returns transient
// plaintext without modifying the credential. Callers must clear the returned
// bytes after use and must not persist or log them.
func openCredentialSecret(ctx context.Context, secretSvc *secrets.Service, credential db.VaultCredential) ([]byte, error) {
	if credential.SecretEnvelope == nil {
		return nil, ErrMissingSecretEnvelope
	}
	plaintext, err := secretSvc.Open(ctx, credentialBinding(credential), *credential.SecretEnvelope)
	if err != nil {
		return nil, fmt.Errorf("open credential secret: %w", err)
	}
	return plaintext, nil
}

func clearCredentialSecretPayload(credential *db.VaultCredential) {
	if credential == nil {
		return
	}
	clear(credential.SecretPayload)
	credential.SecretPayload = nil
}

package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

var ErrInjectionRejected = errors.New("vault credential injection rejected")

// Injector loads session vault credentials per request and rewrites MCP
// Authorization for static_bearer targets. Plaintext tokens are never cached.
type Injector struct {
	db        *db.DB
	secretSvc *secrets.Service
}

func NewInjector(database *db.DB, secretSvc *secrets.Service) *Injector {
	return &Injector{db: database, secretSvc: secretSvc}
}

// RewriteAuthorization: passthrough leaves headers alone; inject sets Bearer;
// reject returns ErrInjectionRejected.
func (i *Injector) RewriteAuthorization(
	ctx context.Context,
	codeSessionExternalID string,
	organizationUUID string,
	workspaceUUID string,
	requestURL *url.URL,
	header http.Header,
) error {
	if i == nil || i.db == nil {
		return nil
	}
	vaultIDs, err := i.db.GetCodeSessionVaultIDs(ctx, codeSessionExternalID, organizationUUID, workspaceUUID)
	if err != nil {
		return fmt.Errorf("%w: load vault_ids: %w", ErrInjectionRejected, err)
	}
	if len(vaultIDs) == 0 {
		return nil
	}
	credentials, err := i.db.ListActiveVaultCredentialsForVaultIDs(ctx, workspaceUUID, vaultIDs)
	if err != nil {
		return fmt.Errorf("%w: load credentials: %w", ErrInjectionRejected, err)
	}
	decision := DecideInjection(requestURL, credentials)
	switch decision.Kind {
	case InjectionPassthrough:
		return nil
	case InjectionInject:
		token, err := openStaticBearerToken(ctx, i.secretSvc, decision.Credential)
		if err != nil {
			return fmt.Errorf("%w: open credential: %w", ErrInjectionRejected, err)
		}
		header.Set("Authorization", "Bearer "+token)
		return nil
	default:
		return ErrInjectionRejected
	}
}

type staticBearerSecret struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

func openStaticBearerToken(ctx context.Context, secretSvc *secrets.Service, credential *db.VaultCredential) (string, error) {
	if credential == nil {
		return "", errors.New("missing credential")
	}
	if err := OpenCredentialSecret(ctx, secretSvc, credential); err != nil {
		return "", err
	}
	defer clearCredentialSecretPayload(credential)

	var secret staticBearerSecret
	if err := json.Unmarshal(credential.SecretPayload, &secret); err != nil {
		return "", fmt.Errorf("decode static_bearer secret: %w", err)
	}
	token := strings.TrimSpace(secret.Token)
	if token == "" || secret.Type != "static_bearer" {
		return "", errors.New("static_bearer secret payload is incomplete")
	}
	return token, nil
}


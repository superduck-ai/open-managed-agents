package vaults

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

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
	decision := decideInjection(requestURL, credentials)
	switch decision.Kind {
	case injectionPassthrough:
		return nil
	case injectionInject:
		token, err := i.openStaticBearerToken(ctx, decision.Credential)
		if err != nil {
			return fmt.Errorf("%w: open credential: %w", ErrInjectionRejected, err)
		}
		header.Set("Authorization", "Bearer "+token)
		return nil
	default:
		return ErrInjectionRejected
	}
}

func (i *Injector) openStaticBearerToken(ctx context.Context, credential *db.VaultCredential) (string, error) {
	if credential == nil {
		return "", errors.New("missing credential")
	}
	plaintext, err := openCredentialSecret(ctx, i.secretSvc, *credential)
	if err != nil {
		return "", err
	}
	defer clear(plaintext)

	secret, err := decodeStaticBearerCredentialSecret(plaintext)
	if err != nil {
		return "", err
	}
	return secret.Token, nil
}

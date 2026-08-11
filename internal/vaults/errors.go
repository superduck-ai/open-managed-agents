package vaults

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// ErrMissingSecretEnvelope is returned when an active credential has no
// secret envelope to open. The Vaults API asks clients to resubmit the secret;
// runtime callers may map it differently when the upstream credential is
// unavailable.
var ErrMissingSecretEnvelope = errors.New("vault credential secret is missing; resubmit the secret")

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func vaultBetaRequired() error {
	return apperr.New(apperr.InvalidArgument, "Vaults API requires beta=true", nil)
}

func routeNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func vaultArchived() error {
	return apperr.New(apperr.InvalidArgument, "Vault is archived", nil)
}

func credentialArchived() error {
	return apperr.New(apperr.InvalidArgument, "Credential is archived", nil)
}

func credentialRequiresMCPOAuth() error {
	return apperr.New(apperr.InvalidArgument, "mcp_oauth_validate requires an mcp_oauth credential", nil)
}

func missingAPIKey() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func vaultNotFound(vaultID string, cause error) error {
	return apperr.New(apperr.NotFound, "Vault not found: "+vaultID, cause)
}

func credentialNotFound(credentialID string, cause error) error {
	return apperr.New(apperr.NotFound, "Credential not found: "+credentialID, cause)
}

func mapCreateCredentialError(err error, vaultID string) error {
	switch {
	case errors.Is(err, db.ErrDuplicate):
		return apperr.New(apperr.Conflict, "Credential key already exists", err)
	case errors.Is(err, db.ErrLimitExceeded):
		return apperr.New(apperr.InvalidArgument, "Vault may contain at most 20 active credentials", err)
	case errors.Is(err, db.ErrNotFound):
		return vaultNotFound(vaultID, err)
	default:
		return internalError("Could not create credential", fmt.Errorf("create vault credential: %w", err))
	}
}

func mapUpdateCredentialError(err error, credentialID string) error {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return credentialNotFound(credentialID, err)
	case errors.Is(err, db.ErrDuplicate):
		return apperr.New(apperr.Conflict, "Credential key already exists", err)
	case errors.Is(err, db.ErrVersionConflict):
		return apperr.New(apperr.Conflict, "Credential was modified concurrently; reload and try again", err)
	default:
		return internalError("Could not update credential", fmt.Errorf("update vault credential %q: %w", credentialID, err))
	}
}

func credentialSecretError(err error, internalMessage string) error {
	if errors.Is(err, ErrMissingSecretEnvelope) {
		return apperr.New(apperr.InvalidArgument, ErrMissingSecretEnvelope.Error(), err)
	}
	return internalError(internalMessage, err)
}

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

// ErrInjectionRejected is the MITM MCP inject fail-closed sentinel: the request
// host is covered by a vault credential but no injectable credential could be used.
// Transport adapters match with errors.Is and must not invent a second wording
// for the client-facing message — use InjectionUnavailablePublicMessage.
var ErrInjectionRejected = errors.New("vault credential injection rejected")

// InjectionUnavailablePublicMessage is the client-safe text for MITM 502
// when MCP credential injection rejects.
const InjectionUnavailablePublicMessage = "MCP upstream credentials are unavailable"

// ErrMITMRequiredForEnvCredentials is returned at Session mount when active
// Environment Variable Credentials are attached but upstream proxy MITM is off.
var ErrMITMRequiredForEnvCredentials = errors.New("upstream proxy MITM is required for environment variable credentials")

// ErrSubstitutionRejected is the upstream-proxy fail-closed sentinel when
// Egress Secret Substitution or Git Smart HTTP Authorization cannot proceed.
var ErrSubstitutionRejected = errors.New("vault environment variable substitution rejected")

// SubstitutionUnavailablePublicMessage is the client-safe text for MITM 502
// when Egress Secret Substitution or Git Smart HTTP Authorization rejects a request.
const SubstitutionUnavailablePublicMessage = "Environment variable credentials are unavailable"

// SubstitutionBodyTooLargePublicMessage is the client-safe text for MITM 502
// when body injection is enabled but the request body exceeds the snapshot buffer.
const SubstitutionBodyTooLargePublicMessage = "Request body exceeds 32 MiB; environment variable body substitution cannot be applied"

// SubstitutionPublicMessage returns the MITM 502 text for a substitution rejection.
func SubstitutionPublicMessage(err error) string {
	if errors.Is(err, errSnapshotRequestBodyTooLarge) {
		return SubstitutionBodyTooLargePublicMessage
	}
	return SubstitutionUnavailablePublicMessage
}

var (
	errMCPOAuthRefreshUnavailable = errors.New("mcp_oauth refresh unavailable")
	errCredentialStoreUnavailable = errors.New("credential store is unavailable")
)

func injectionRejected(cause error) error {
	if cause == nil {
		return ErrInjectionRejected
	}
	return fmt.Errorf("%w: %w", ErrInjectionRejected, cause)
}

func substitutionRejected(cause error) error {
	if cause == nil {
		return ErrSubstitutionRejected
	}
	return fmt.Errorf("%w: %w", ErrSubstitutionRejected, cause)
}

func missingCredential() error {
	return errors.New("missing credential")
}

func incompleteMCPOAuthSecret() error {
	return errors.New("mcp_oauth secret payload is incomplete")
}

func emptyMCPOAuthAuth() error {
	return errors.New("empty mcp_oauth auth")
}

func credentialAuthNotMCPOAuth() error {
	return errors.New("credential auth is not mcp_oauth")
}

func mcpOAuthServerURLRequired() error {
	return errors.New("mcp_oauth auth mcp_server_url is required")
}

func credentialTypeNotInjectable(authType string) error {
	return fmt.Errorf("credential type %q is not injectable", authType)
}

func tokenEndpointAuthMissingSecret(method string) error {
	return fmt.Errorf("%s selected without client secret", method)
}

func unsupportedTokenAuthMethod(method string) error {
	return fmt.Errorf("unsupported token auth method %q", method)
}

func tokenEndpointStatus(status int) error {
	return fmt.Errorf("token endpoint status %d", status)
}

func tokenEndpointMissingAccessToken() error {
	return errors.New("token endpoint returned no access_token")
}

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

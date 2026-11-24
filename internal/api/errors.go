package api

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

const (
	envOAuthErrorInvalidRequest        = "invalid_request"
	envOAuthErrorProviderUnavailable   = "platform_oauth_provider_unavailable"
	envOAuthErrorCredentialKeyConflict = "credential_key_conflict"
	envOAuthErrorTokenExchangeFailed   = "token_exchange_failed"
	envOAuthErrorFlowInProgress        = "flow_in_progress"
	envOAuthErrorCredentialSaveFailed  = "credential_save_failed"
	envOAuthErrorCredentialLimit       = "credential_limit_exceeded"
	envOAuthErrorCredentialVersion     = "credential_version_conflict"
	envOAuthErrorCredentialUnavailable = "credential_unavailable"
)

func envOAuthCredentialSaveErrorCode(err error) string {
	switch {
	case errors.Is(err, db.ErrDuplicate):
		return envOAuthErrorCredentialKeyConflict
	case errors.Is(err, db.ErrLimitExceeded):
		return envOAuthErrorCredentialLimit
	case errors.Is(err, db.ErrVersionConflict):
		return envOAuthErrorCredentialVersion
	case errors.Is(err, db.ErrNotFound):
		return envOAuthErrorCredentialUnavailable
	default:
		return envOAuthErrorCredentialSaveFailed
	}
}

func platformIdentityUnauthorized() *httpapi.Error {
	return httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid or missing session")
}

func platformIdentityUnavailable() *httpapi.Error {
	return httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
}

func platformOrganizationDenied() *httpapi.Error {
	return httpapi.NewError(http.StatusForbidden, "permission_error", "Organization not allowed")
}

func platformCSRFRejected() *httpapi.Error {
	return httpapi.NewError(http.StatusForbidden, "permission_error", "Invalid CSRF token")
}

func platformOrganizationInvalid() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Invalid or conflicting organization scope")
}
func platformWorkspaceNotFound() *httpapi.Error {
	return httpapi.NewError(http.StatusNotFound, "not_found_error", "Workspace not found")
}

func platformOrganizationNotFound() *httpapi.Error {
	return httpapi.NewError(http.StatusNotFound, "not_found_error", "Organization not found")
}
func platformAuthorizationUnavailable() *httpapi.Error {
	return httpapi.NewError(http.StatusServiceUnavailable, "api_error", "Platform authorization unavailable")
}

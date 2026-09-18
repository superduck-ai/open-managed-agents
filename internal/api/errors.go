package api

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

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

package admin

import (
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
	"net/http"
)

type serviceError struct {
	status  int
	typ     string
	message string
}

func organizationAdministratorRequired() error {
	return &serviceError{status: http.StatusForbidden, typ: "permission_error", message: "Organization administrator required"}
}

func (e *serviceError) Error() string {
	return e.message
}

func mapAdminDBError(err error, missingMessage string) error {
	if errors.Is(err, db.ErrLastOrganizationAdmin) {
		return conflict(err.Error())
	}
	if errors.Is(err, workspaceaccess.ErrDenied) {
		return &serviceError{status: http.StatusForbidden, typ: "permission_error", message: "Action not allowed"}
	}
	if errors.Is(err, workspaceaccess.ErrDefaultProtected) || errors.Is(err, workspaceaccess.ErrInheritedRole) || errors.Is(err, workspaceaccess.ErrInvalidRole) {
		return invalidRequest(err.Error())
	}
	if errors.Is(err, db.ErrNotFound) {
		return notFound(missingMessage)
	}
	if errors.Is(err, db.ErrDuplicate) {
		return conflict("Resource already exists")
	}
	return err
}

func invalidRequest(message string) error {
	return &serviceError{status: http.StatusBadRequest, typ: "invalid_request_error", message: message}
}

func notFound(message string) error {
	return &serviceError{status: http.StatusNotFound, typ: "not_found_error", message: message}
}

func conflict(message string) error {
	return &serviceError{status: http.StatusConflict, typ: "conflict_error", message: message}
}

func authenticatedPrincipalRequired() error {
	return &serviceError{status: http.StatusUnauthorized, typ: "authentication_error", message: "Missing authenticated principal"}
}
func billingAccessRequired() error {
	return &serviceError{status: http.StatusForbidden, typ: "permission_error", message: "Organization billing access required"}
}

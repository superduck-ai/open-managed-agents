package files

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func invalidFilePageError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Invalid page cursor")
}

func conflictingFilePageError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "page cannot be combined with after_id or before_id")
}

func platformFileWorkspaceError(err error) *httpapi.Error {
	if errors.Is(err, workspaceaccess.ErrDenied) {
		return httpapi.NewError(http.StatusForbidden, "permission_error", "Workspace not allowed")
	}
	return httpapi.NewError(http.StatusInternalServerError, "api_error", "Workspace authorization failed")
}

func fileRuntimeUnavailableError(cause error) error {
	return apperr.New(apperr.Unavailable, "Workspace runtime files are unavailable", cause)
}

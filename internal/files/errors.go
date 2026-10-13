package files

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func platformFileWorkspaceError(err error) *httpapi.Error {
	if errors.Is(err, workspaceaccess.ErrDenied) {
		return httpapi.NewError(http.StatusForbidden, "permission_error", "Workspace not allowed")
	}
	return httpapi.NewError(http.StatusInternalServerError, "api_error", "Workspace authorization failed")
}

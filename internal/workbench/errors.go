package workbench

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func writeWorkbenchWorkspaceError(w http.ResponseWriter, r *http.Request, err error) {
	status, typ, message := http.StatusInternalServerError, "api_error", "Workspace authorization failed"
	if errors.Is(err, workspaceaccess.ErrDenied) {
		status, typ, message = http.StatusForbidden, "permission_error", "Workspace not allowed"
	}
	httpapi.WriteError(w, r, httpapi.NewError(status, typ, message))
}

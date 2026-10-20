package workbench

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func writeWorkbenchWorkspaceError(w http.ResponseWriter, r *http.Request, err error) {
	status, typ, message := http.StatusInternalServerError, "api_error", "Workspace authorization failed"
	if errors.Is(err, workspaceaccess.ErrDenied) {
		status, typ, message = http.StatusForbidden, "permission_error", "Workspace not allowed"
	}
	httpapi.WriteError(w, r, httpapi.NewError(status, typ, message))
}

func writeWorkbenchInferenceError(w http.ResponseWriter, err error) {
	message := err.Error()
	switch {
	case errors.Is(err, llmproviders.ErrBackendMismatch):
		writeProxyMessagesAnthropicError(w, http.StatusBadRequest, "invalid_request_error", llmproviders.ErrBackendMismatch.Error())
	case errors.Is(err, llmproviders.ErrModelNotConfigured), strings.Contains(message, "model is required"):
		writeProxyMessagesAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "Model is not configured for this workspace")
	case errors.Is(err, llmproviders.ErrNotConfigured):
		writeProxyMessagesAnthropicError(w, http.StatusServiceUnavailable, "api_error", "This workspace has no LLM provider configured")
	case errors.Is(err, llmproviders.ErrInvalidBaseURL),
		errors.Is(err, llmproviders.ErrUnsafeBaseURL),
		message == "LLM provider request failed",
		strings.HasPrefix(message, "LLM provider returned"):
		writeProxyMessagesAnthropicError(w, http.StatusBadGateway, "api_error", "workspace LLM provider is unavailable")
	default:
		writeProxyMessagesAnthropicError(w, http.StatusInternalServerError, "api_error", "Workspace model configuration is unavailable")
	}
}

package mcpservers

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type endpoint func(http.ResponseWriter, *http.Request) error

type consoleErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (h *Handler) wrap(next endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := next(w, r); err != nil {
			h.writeError(w, r, err)
		}
	}
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := errors.AsType[*apperr.Error](err)
	status, code, valid := consoleErrorMapping(appErr)
	if !ok || !valid {
		h.logError(r, err)
		httpapi.WriteJSON(w, http.StatusInternalServerError, consoleErrorResponse{
			Error: "internal_error", Message: internalServerErrorMessage,
		})
		return
	}
	if status >= http.StatusInternalServerError {
		h.logError(r, err)
	}
	httpapi.WriteJSON(w, status, consoleErrorResponse{Error: code, Message: appErr.PublicMessage})
}

func (h *Handler) logError(r *http.Request, err error) {
	h.logger.ErrorContext(
		r.Context(),
		"workspace MCP server request failed",
		"request_id", httpapi.RequestID(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
		"error", err,
	)
}

func consoleErrorMapping(err *apperr.Error) (int, string, bool) {
	if err == nil || err.PublicMessage == "" {
		return 0, "", false
	}
	switch err.Kind {
	case apperr.InvalidArgument:
		return http.StatusBadRequest, "invalid_request", true
	case apperr.NotFound:
		return http.StatusNotFound, "not_found", true
	case apperr.Conflict:
		return http.StatusConflict, "conflict", true
	case apperr.Internal:
		return http.StatusInternalServerError, "internal_error", true
	default:
		return 0, "", false
	}
}

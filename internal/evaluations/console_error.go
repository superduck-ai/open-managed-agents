package evaluations

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type endpoint func(http.ResponseWriter, *http.Request) error

type errorBody struct {
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
	status, code, message, ok := consoleStatus(err)
	if !ok {
		status, code, message = http.StatusInternalServerError, "internal_error", internalServerErrorMessage
	}
	if status >= http.StatusInternalServerError {
		h.logger.ErrorContext(r.Context(), "evaluation request failed",
			"request_id", httpapi.RequestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"error", err,
		)
	}
	httpapi.WriteJSON(w, status, errorBody{Error: code, Message: message})
}

func consoleStatus(err error) (int, string, string, bool) {
	appErr, ok := errors.AsType[*apperr.Error](err)
	if !ok || appErr == nil || appErr.PublicMessage == "" {
		return 0, "", "", false
	}
	switch appErr.Kind {
	case apperr.InvalidArgument:
		return http.StatusBadRequest, "invalid_request", appErr.PublicMessage, true
	case apperr.NotFound:
		return http.StatusNotFound, "not_found", appErr.PublicMessage, true
	case apperr.Conflict:
		return http.StatusConflict, "conflict", appErr.PublicMessage, true
	case apperr.Internal:
		return http.StatusInternalServerError, "internal_error", appErr.PublicMessage, true
	default:
		return 0, "", "", false
	}
}

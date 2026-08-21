package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

// Endpoint is an HTTP endpoint that may return an error before committing a response.
type Endpoint func(http.ResponseWriter, *http.Request) error

// ErrorAdapter converts application errors into Anthropic-compatible HTTP errors.
type ErrorAdapter struct {
	logger *slog.Logger
}

// NewErrorAdapter creates a final HTTP error boundary.
func NewErrorAdapter(logger *slog.Logger) *ErrorAdapter {
	return &ErrorAdapter{logger: logging.LoggerOrDefault(logger)}
}

// Wrap adapts an error-returning endpoint to net/http.
func (a *ErrorAdapter) Wrap(next Endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := next(w, r); err != nil {
			a.Write(w, r, err)
		}
	}
}

// Write records a final failure when necessary and writes its safe HTTP response.
func (a *ErrorAdapter) Write(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := errors.AsType[*apperr.Error](err)
	mapping, valid := errorMappingFor(appErr)
	if !ok || !valid {
		a.log(r, "internal", err)
		WriteError(w, r, NewError(http.StatusInternalServerError, "api_error", "Internal server error"))
		return
	}
	if mapping.status >= http.StatusInternalServerError {
		a.log(r, mapping.kind, err)
	}
	transportError := NewError(mapping.status, mapping.errorType, appErr.PublicMessage)
	transportError.Code = appErr.Code
	WriteError(w, r, transportError)
}

func (a *ErrorAdapter) log(r *http.Request, kind string, err error) {
	a.logger.ErrorContext(
		r.Context(),
		"http request failed",
		"request_id", RequestID(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
		"error_kind", kind,
		"error", err,
	)
}

type errorMapping struct {
	status    int
	errorType string
	kind      string
}

func errorMappingFor(err *apperr.Error) (errorMapping, bool) {
	if err == nil || strings.TrimSpace(err.PublicMessage) == "" {
		return errorMapping{}, false
	}

	switch err.Kind {
	case apperr.InvalidArgument:
		return errorMapping{http.StatusBadRequest, "invalid_request_error", "invalid_argument"}, true
	case apperr.InvalidState:
		return errorMapping{http.StatusConflict, "invalid_request_error", "invalid_state"}, true
	case apperr.PreconditionFailed:
		return errorMapping{http.StatusPreconditionFailed, "invalid_request_error", "precondition_failed"}, true
	case apperr.RequestTooLarge:
		return errorMapping{http.StatusRequestEntityTooLarge, "invalid_request_error", "request_too_large"}, true
	case apperr.Unauthenticated:
		return errorMapping{http.StatusUnauthorized, "authentication_error", "unauthenticated"}, true
	case apperr.Billing:
		return errorMapping{http.StatusPaymentRequired, "billing_error", "billing"}, true
	case apperr.PermissionDenied:
		return errorMapping{http.StatusForbidden, "permission_error", "permission_denied"}, true
	case apperr.NotFound:
		return errorMapping{http.StatusNotFound, "not_found_error", "not_found"}, true
	case apperr.Conflict:
		return errorMapping{http.StatusConflict, "conflict_error", "conflict"}, true
	case apperr.RateLimited:
		return errorMapping{http.StatusTooManyRequests, "rate_limit_error", "rate_limited"}, true
	case apperr.Timeout:
		return errorMapping{http.StatusGatewayTimeout, "timeout_error", "timeout"}, true
	case apperr.Internal:
		return errorMapping{http.StatusInternalServerError, "api_error", "internal"}, true
	case apperr.Unavailable:
		return errorMapping{http.StatusServiceUnavailable, "api_error", "unavailable"}, true
	case apperr.Overloaded:
		return errorMapping{529, "overloaded_error", "overloaded"}, true
	default:
		return errorMapping{}, false
	}
}

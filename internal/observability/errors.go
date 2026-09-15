package observability

import (
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func errInvalidArgument(message string) error {
	return apperr.New(apperr.InvalidArgument, message, nil)
}

func errNotFound(message string) error {
	return apperr.New(apperr.NotFound, message, nil)
}

func errTimeout(message string, cause error) error {
	return apperr.New(apperr.Timeout, message, cause)
}

func errUnavailable(message string, cause error) error {
	return apperr.New(apperr.Unavailable, message, cause)
}

func errInternal(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func errRequestTooLarge(message string) error {
	return apperr.New(apperr.RequestTooLarge, message, nil)
}

func QueryTimeout(cause error) error {
	return errTimeout("observability query timed out", cause)
}

func QueryUnavailable(cause error) error {
	return errUnavailable("observability query is unavailable", cause)
}

func QueryInternal(message string, cause error) error {
	return errInternal(message, cause)
}

func MissingScopePlaceholder() error {
	return errMissingScopePlaceholder()
}

func UnresolvedPlaceholder(placeholder string) error {
	return errUnresolvedPlaceholder(placeholder)
}

func InvalidLiteral(message string) error {
	return errInvalidArgument(message)
}

func QueryRequestTooLarge() error {
	return errRequestTooLarge("observability query request is too large")
}

func errQueryRefNotFound(queryRef string) error {
	return errNotFound(fmt.Sprintf("query_ref %q was not found", queryRef))
}

func errAgentNotFound() error {
	return errNotFound("agent not found")
}

func errSessionNotFound() error {
	return errNotFound("session not found")
}

func errMissingScopePlaceholder() error {
	return errInternal("observability query is missing a tenant scope placeholder", nil)
}

func errUnresolvedPlaceholder(placeholder string) error {
	return errInternal(fmt.Sprintf("observability query has unresolved placeholder %s", placeholder), nil)
}

func errInvalidRequestBody() error {
	return errInvalidArgument("request body must be a JSON object")
}

func errQueryRefRequired() error {
	return errInvalidArgument("query_ref is required")
}

func errInvalidStatus() error {
	return errInvalidArgument("status must be ok or error")
}

func errInvalidOffset() error {
	return errInvalidArgument("offset must be a non-negative integer")
}

func errTraceIDRequired() error {
	return errInvalidArgument("trace_id is required")
}

package codesessions

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func codeSessionNotFound(cause error) error {
	return apperr.New(apperr.NotFound, "Code session not found", cause)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func codeSessionRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func sessionIngressTokenRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing session ingress token", nil)
}

func sessionIngressTokenInvalid(cause error) error {
	return apperr.New(apperr.Unauthenticated, "Invalid session ingress token", cause)
}

func invalidSignCommitRequest(message string, cause error) error {
	return apperr.New(apperr.InvalidArgument, message, cause)
}

func signCommitRequestTooLarge(cause error) error {
	return apperr.New(apperr.RequestTooLarge, "Request body exceeds maximum size", cause)
}

func signCommitFailure(cause error) error {
	return internalError("Could not sign commit", cause)
}

func codeSessionEventsLoadError(err error, codeSessionID string) error {
	return internalError(
		"Could not list code session events",
		fmt.Errorf("list code session %q events: %w", codeSessionID, err),
	)
}

func workerEventStreamUnavailable(cause error) error {
	return internalError("Could not connect code session worker stream", cause)
}

func mapCodeSessionLoadError(err error, codeSessionID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return codeSessionNotFound(err)
	}
	return internalError(
		"Could not load code session",
		fmt.Errorf("load code session %q: %w", codeSessionID, err),
	)
}

func invalidWorkerPayload(message string, cause error) error {
	return &workerPayloadError{message: message, cause: cause}
}

type workerPayloadError struct {
	message string
	cause   error
}

func (e *workerPayloadError) Error() string { return e.message }

func (e *workerPayloadError) Unwrap() error { return e.cause }

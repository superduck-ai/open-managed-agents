package batches

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

type modelValidationError struct {
	cause error
}

func newModelValidationError(cause error) *modelValidationError {
	return &modelValidationError{cause: cause}
}

func (e *modelValidationError) Error() string {
	return e.cause.Error()
}

func (e *modelValidationError) Unwrap() error {
	return e.cause
}

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func requestTooLarge(cause error) error {
	return apperr.New(apperr.RequestTooLarge, "Request body exceeds maximum size", cause)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func batchServiceUnavailable(cause error) error {
	return apperr.New(
		apperr.Unavailable,
		"This workspace has no available LLM provider for Message Batches",
		cause,
	)
}

func configuredModelError(err error) error {
	if errors.Is(err, llmproviders.ErrNotConfigured) || errors.Is(err, llmproviders.ErrAmbiguousModel) {
		return batchServiceUnavailable(err)
	}
	if _, ok := errors.AsType[*modelValidationError](err); ok {
		return invalidRequest(err)
	}
	return internalError("Could not validate batch models", fmt.Errorf("validate configured batch models: %w", err))
}

func batchBetaRequired() error {
	return apperr.New(apperr.InvalidArgument, "Message Batches beta requires anthropic-beta: message-batches-2024-09-24", nil)
}

func batchRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func batchAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func messageBatchNotFound(batchID string, cause error) error {
	return apperr.New(apperr.NotFound, "Message batch not found: "+batchID, cause)
}

func messageBatchMustBeEnded(cause error) error {
	return apperr.New(apperr.InvalidState, "Message batch must be ended before deletion", cause)
}

func messageBatchHasNotEnded() error {
	return apperr.New(apperr.InvalidArgument, "Message batch has not ended", nil)
}

func messageBatchResultsUnavailable() error {
	return apperr.New(apperr.NotFound, "Message batch results are not available", nil)
}

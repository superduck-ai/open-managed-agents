package batches

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func requestTooLarge(cause error) error {
	return apperr.New(apperr.RequestTooLarge, "Request body exceeds maximum size", cause)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func batchServiceUnavailable() error {
	return apperr.New(
		apperr.Unavailable,
		"anthropic_upstream.api_key is required for Message Batches",
		errors.New("anthropic_upstream.api_key is empty"),
	)
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

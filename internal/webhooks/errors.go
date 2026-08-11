package webhooks

import "github.com/superduck-ai/open-managed-agents/internal/apperr"

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func webhooksBetaRequired() error {
	return apperr.New(apperr.InvalidArgument, "Webhooks API requires anthropic-beta: webhooks-2026-03-01", nil)
}

func webhookRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func webhookAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func webhookNotFound(webhookID string, cause error) error {
	return apperr.New(apperr.NotFound, "Webhook not found: "+webhookID, cause)
}

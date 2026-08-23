package messages

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func authenticationRequiredError() *httpapi.Error {
	return httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key")
}

func invalidRequestError(cause error) *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", cause.Error())
}

func requestTooLargeError() *httpapi.Error {
	return httpapi.NewError(http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds maximum size")
}

func upstreamUnavailableError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadGateway, "api_error", "Messages upstream is unavailable")
}

func modelNotConfiguredError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Model is not configured for this workspace")
}

func providerNotConfiguredError() *httpapi.Error {
	return httpapi.NewError(http.StatusServiceUnavailable, "api_error", "This workspace has no LLM provider configured")
}

func providerConfigurationUnavailableError() *httpapi.Error {
	return httpapi.NewError(http.StatusInternalServerError, "api_error", "Workspace model configuration is unavailable")
}

func providerResolveError(err error) *httpapi.Error {
	switch {
	case errors.Is(err, llmproviders.ErrModelNotConfigured):
		return modelNotConfiguredError()
	case errors.Is(err, llmproviders.ErrNotConfigured):
		return providerNotConfiguredError()
	default:
		return providerConfigurationUnavailableError()
	}
}

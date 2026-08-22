package messages

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
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

func providerUnavailableError() *httpapi.Error {
	return httpapi.NewError(http.StatusServiceUnavailable, "api_error", "The workspace LLM provider is unavailable")
}

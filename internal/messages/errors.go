package messages

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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

func upstreamAuthenticationRejectedError() *httpapi.Error {
	return httpapi.NewError(http.StatusForbidden, "permission_error", "Messages upstream rejected its configured credentials")
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
	case errors.Is(err, llmproviders.ErrAmbiguousModel):
		return invalidRequestError(err)
	case errors.Is(err, llmproviders.ErrNotConfigured):
		return providerNotConfiguredError()
	default:
		return providerConfigurationUnavailableError()
	}
}

func fileReferenceNotFoundError() *httpapi.Error {
	return httpapi.NewError(http.StatusNotFound, "not_found_error", "Referenced file was not found")
}
func fileReferenceUnavailableError() *httpapi.Error {
	return httpapi.NewError(http.StatusServiceUnavailable, "api_error", "Referenced file storage is unavailable")
}
func fileReferenceTooLargeError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Referenced file exceeds the model request size limit")
}
func fileReferenceInvalidTypeError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Referenced file type does not match its content block")
}
func fileReferenceInvalidTextError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Referenced text file must contain valid UTF-8")
}
func fileReferenceInvalidRequestError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Invalid file reference content")
}

func backendMismatchError() *httpapi.Error {
	return invalidRequestError(llmproviders.ErrBackendMismatch)
}

func brainRequiredError() *httpapi.Error {
	return httpapi.NewError(http.StatusForbidden, "permission_error", "Chat Completions is only available to brain model credentials")
}

var errChatStreamRequired = errors.New("brain Chat Completions requires stream=true and n=1")

func chatSessionLookupError(cause error) *httpapi.Error {
	if errors.Is(cause, db.ErrNotFound) {
		return httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid API key")
	}
	return providerConfigurationUnavailableError()
}

func modelRequestStartError(cause error) *httpapi.Error {
	if errors.Is(cause, db.ErrSessionBudgetReached) {
		return &httpapi.Error{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "session_budget_reached", Message: "session budget reached: change or remove the budget before starting another model request"}
	}
	if errors.Is(cause, codesessions.ErrModelPriceUnavailable) {
		return &httpapi.Error{Status: http.StatusServiceUnavailable, Type: "api_error", Code: "model_price_unavailable", Message: "model price unavailable for budgeted session"}
	}
	return upstreamUnavailableError()
}

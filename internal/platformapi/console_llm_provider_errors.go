package platformapi

import (
	"net/http"
)

const (
	llmProviderCodeAPIKeyRequired       = "api_key_required"
	llmProviderCodeBaseURLInvalid       = "base_url_invalid"
	llmProviderCodeBaseURLUnsafe        = "base_url_unsafe"
	llmProviderCodeConfigurationInvalid = "llm_provider_configuration_invalid"
	llmProviderCodeModelIDInvalid       = "model_id_invalid"
	llmProviderCodeModelIDsDuplicate    = "model_ids_duplicate"
	llmProviderCodeModelIDsLimit        = "model_ids_limit"
	llmProviderCodeNameConflict         = "llm_provider_name_conflict"
	llmProviderCodeNameInvalid          = "llm_provider_name_invalid"
	llmProviderCodeNotFound             = "llm_provider_not_found"
	llmProviderCodeSlugConflict         = "llm_provider_slug_conflict"
	llmProviderCodeSlugImmutable        = "llm_provider_slug_immutable"
	llmProviderCodeSlugInvalid          = "llm_provider_slug_invalid"
	llmProviderCodePermissionDenied     = "llm_provider_permission_denied"
	llmProviderCodeUpstreamUnavailable  = "upstream_models_unavailable"
)

type llmProviderErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type llmProviderInputError struct {
	code    string
	message string
}

func newLLMProviderInputError(code, message string) *llmProviderInputError {
	return &llmProviderInputError{code: code, message: message}
}

func (e *llmProviderInputError) Error() string {
	return e.message
}

func writeLLMProviderError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, llmProviderErrorResponse{Error: "invalid_request", Code: code, Message: message})
}

func writeLLMProviderPermissionDenied(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, llmProviderErrorResponse{
		Error:   "permission_denied",
		Code:    llmProviderCodePermissionDenied,
		Message: "Administrator access is required to manage LLM providers",
	})
}

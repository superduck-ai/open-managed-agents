package platformapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"

	"github.com/go-chi/chi/v5"
)

type previewLLMProviderModelsRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type previewLLMProviderModelsResponse struct {
	ModelIDs []string `json:"model_ids"`
}

func previewConsoleLLMProviderModels(database *db.DB, client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := llmProviderScope(w, r, database); !ok {
			return
		}
		body, err := readRequiredJSON[previewLLMProviderModelsRequest](r, true)
		if err != nil {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeConfigurationInvalid, "request body must match LLM provider configuration")
			return
		}
		apiKey := strings.TrimSpace(body.APIKey)
		if apiKey == "" {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeAPIKeyRequired, "api_key is required")
			return
		}
		baseURL, err := llmproviders.ValidateBaseURL(body.BaseURL)
		if err != nil {
			code := llmProviderCodeBaseURLInvalid
			if errors.Is(err, llmproviders.ErrUnsafeBaseURL) {
				code = llmProviderCodeBaseURLUnsafe
			}
			writeLLMProviderError(w, http.StatusBadRequest, code, err.Error())
			return
		}
		modelIDs, err := llmproviders.ListUpstreamModels(r.Context(), client, llmproviders.Upstream{
			BaseURL: baseURL,
			APIKey:  apiKey,
		})
		if err != nil {
			writeLLMProviderError(w, http.StatusBadGateway, llmProviderCodeUpstreamUnavailable, "failed to list models from provider")
			return
		}
		writeJSON(w, http.StatusOK, previewLLMProviderModelsResponse{
			ModelIDs: llmproviders.MergeModelIDs(nil, modelIDs, maxLLMProviderModels),
		})
	}
}

func syncConsoleLLMProviderModels(database *db.DB, secretService *secrets.Service, client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, scope, ok := llmProviderScope(w, r, database)
		if !ok {
			return
		}
		provider, err := database.GetLLMProvider(
			r.Context(),
			orgUUID,
			scope.UUID,
			strings.TrimSpace(chi.URLParam(r, "providerId")),
		)
		if errors.Is(err, db.ErrNotFound) {
			writeLLMProviderError(w, http.StatusNotFound, llmProviderCodeNotFound, "LLM provider not found")
			return
		}
		if err != nil {
			internalError(w, "failed to load LLM provider")
			return
		}
		if secretService == nil || provider.SecretEnvelope == nil {
			internalError(w, "LLM provider encryption is unavailable")
			return
		}
		plaintext, err := secretService.Open(r.Context(), llmproviders.SecretBinding(provider), *provider.SecretEnvelope)
		if err != nil {
			internalError(w, "failed to decrypt LLM provider API key")
			return
		}
		defer clear(plaintext)
		liveIDs, err := llmproviders.ListUpstreamModels(r.Context(), client, llmproviders.Upstream{
			BaseURL: provider.BaseURL,
			APIKey:  string(plaintext),
		})
		if err != nil {
			writeLLMProviderError(w, http.StatusBadGateway, llmProviderCodeUpstreamUnavailable, "failed to list models from provider")
			return
		}
		incomingIDs, skippedModelIDs, err := excludeConfiguredModels(
			r.Context(),
			database,
			orgUUID,
			scope.UUID,
			provider.ExternalID,
			liveIDs,
		)
		if err != nil {
			internalError(w, "failed to validate LLM provider models")
			return
		}
		modelIDs := llmproviders.MergeModelIDs(provider.ModelIDs, incomingIDs, maxLLMProviderModels)
		if slices.Equal(provider.ModelIDs, modelIDs) {
			response := formatLLMProvider(provider)
			response.SkippedModelIDs = skippedModelIDs
			writeJSON(w, http.StatusOK, response)
			return
		}
		provider.ModelIDs = modelIDs
		provider.UpdatedAt = time.Now().UTC()
		updated, err := database.UpdateLLMProvider(r.Context(), provider)
		if writeLLMProviderModelConflictError(w, err) {
			return
		}
		if err != nil {
			internalError(w, "failed to update LLM provider")
			return
		}
		response := formatLLMProvider(updated)
		response.SkippedModelIDs = skippedModelIDs
		writeJSON(w, http.StatusOK, response)
	}
}

func excludeConfiguredModels(
	ctx context.Context,
	database *db.DB,
	organizationUUID, workspaceUUID, excludedProviderID string,
	modelIDs []string,
) ([]string, []string, error) {
	configured, err := configuredLLMModelIDs(ctx, database, organizationUUID, workspaceUUID, excludedProviderID)
	if err != nil {
		return nil, nil, err
	}
	filtered := make([]string, 0, len(modelIDs))
	skipped := make([]string, 0)
	for _, modelID := range modelIDs {
		if _, exists := configured[modelID]; exists {
			skipped = append(skipped, modelID)
			continue
		}
		filtered = append(filtered, modelID)
	}
	return filtered, skipped, nil
}

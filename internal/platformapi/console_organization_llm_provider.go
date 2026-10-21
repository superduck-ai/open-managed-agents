package platformapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func getConsoleOrganizationLLMProvider(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		provider, err := database.GetOrganizationLLMProvider(r.Context(), orgUUID)
		if errors.Is(err, db.ErrNotFound) {
			writeLLMProviderError(w, http.StatusNotFound, llmProviderCodeNotFound, "LLM provider not found")
			return
		}
		if err != nil {
			internalError(w, "failed to load LLM provider")
			return
		}
		writeJSON(w, http.StatusOK, formatOrganizationLLMProvider(provider))
	}
}

func putConsoleOrganizationLLMProvider(database *db.DB, secretService *secrets.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := llmProviderOrganization(w, r, database)
		if !ok {
			return
		}
		body, ok := decodeLLMProviderRequest(w, r)
		if !ok {
			return
		}
		if body.Slug != nil && strings.TrimSpace(*body.Slug) != "" && strings.TrimSpace(*body.Slug) != llmproviders.OrganizationSlug {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeSlugImmutable, "organization provider slug must be organization")
			return
		}
		name, baseURL, modelIDs, ok := validateLLMProviderInput(w, body)
		if !ok {
			return
		}
		current, err := database.GetOrganizationLLMProvider(r.Context(), orgUUID)
		if errors.Is(err, db.ErrNotFound) {
			createConsoleOrganizationLLMProvider(w, r, database, secretService, orgUUID, body, name, baseURL, modelIDs)
			return
		}
		if err != nil {
			internalError(w, "failed to load LLM provider")
			return
		}
		updateConsoleOrganizationLLMProvider(w, r, database, secretService, current, body, name, baseURL, modelIDs)
	}
}

func createConsoleOrganizationLLMProvider(
	w http.ResponseWriter,
	r *http.Request,
	database *db.DB,
	secretService *secrets.Service,
	orgUUID string,
	body llmProviderRequest,
	name, baseURL string,
	modelIDs []string,
) {
	if body.APIKey == nil || strings.TrimSpace(*body.APIKey) == "" {
		writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeAPIKeyRequired, "api_key is required")
		return
	}
	apiKey := strings.TrimSpace(*body.APIKey)
	externalID, err := ids.New("llmprov_")
	if err != nil {
		internalError(w, "failed to create LLM provider")
		return
	}
	now := time.Now().UTC()
	provider := db.LLMProvider{
		UUID:             uuid.New().String(),
		ExternalID:       externalID,
		OrganizationUUID: orgUUID,
		Name:             name,
		Slug:             llmproviders.OrganizationSlug,
		APIBackend:       llmProviderBackend(body.APIBackend, llmproviders.Messages),
		BaseURL:          baseURL,
		APIKeyLast4:      lastFour(apiKey),
		ModelIDs:         modelIDs,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if !sealLLMProviderAPIKey(w, r, secretService, &provider, apiKey) {
		return
	}
	created, err := database.CreateLLMProvider(r.Context(), provider)
	if errors.Is(err, db.ErrOrganizationLLMProviderExists) {
		writeLLMProviderError(w, http.StatusConflict, llmProviderCodeOrganizationExists, "organization default provider already exists")
		return
	}
	if err != nil {
		internalError(w, "failed to create LLM provider")
		return
	}
	writeJSON(w, http.StatusCreated, formatOrganizationLLMProvider(created))
}

func updateConsoleOrganizationLLMProvider(
	w http.ResponseWriter,
	r *http.Request,
	database *db.DB,
	secretService *secrets.Service,
	current db.LLMProvider,
	body llmProviderRequest,
	name, baseURL string,
	modelIDs []string,
) {
	current.APIBackend = llmProviderBackend(body.APIBackend, current.APIBackend)
	current.Name = name
	current.BaseURL = baseURL
	current.ModelIDs = modelIDs
	current.UpdatedAt = time.Now().UTC()
	if body.APIKey != nil && strings.TrimSpace(*body.APIKey) != "" {
		apiKey := strings.TrimSpace(*body.APIKey)
		current.APIKeyLast4 = lastFour(apiKey)
		if !sealLLMProviderAPIKey(w, r, secretService, &current, apiKey) {
			return
		}
	}
	updated, err := database.UpdateLLMProvider(r.Context(), current)
	if errors.Is(err, db.ErrNotFound) {
		writeLLMProviderError(w, http.StatusNotFound, llmProviderCodeNotFound, "LLM provider not found")
		return
	}
	if err != nil {
		internalError(w, "failed to update LLM provider")
		return
	}
	writeJSON(w, http.StatusOK, formatOrganizationLLMProvider(updated))
}

func deleteConsoleOrganizationLLMProvider(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := llmProviderOrganization(w, r, database)
		if !ok {
			return
		}
		provider, err := database.GetOrganizationLLMProvider(r.Context(), orgUUID)
		if errors.Is(err, db.ErrNotFound) {
			writeLLMProviderError(w, http.StatusNotFound, llmProviderCodeNotFound, "LLM provider not found")
			return
		}
		if err != nil {
			internalError(w, "failed to load LLM provider")
			return
		}
		err = database.DeleteLLMProvider(r.Context(), orgUUID, "", provider.ExternalID)
		if errors.Is(err, db.ErrNotFound) {
			writeLLMProviderError(w, http.StatusNotFound, llmProviderCodeNotFound, "LLM provider not found")
			return
		}
		if err != nil {
			internalError(w, "failed to delete LLM provider")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func syncConsoleOrganizationLLMProviderModels(database *db.DB, secretService *secrets.Service, client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := llmProviderOrganization(w, r, database)
		if !ok {
			return
		}
		provider, err := database.GetOrganizationLLMProvider(r.Context(), orgUUID)
		if errors.Is(err, db.ErrNotFound) {
			writeLLMProviderError(w, http.StatusNotFound, llmProviderCodeNotFound, "LLM provider not found")
			return
		}
		if err != nil {
			internalError(w, "failed to load LLM provider")
			return
		}
		syncProviderFromUpstream(w, r, database, secretService, client, provider)
	}
}

func llmProviderOrganization(w http.ResponseWriter, r *http.Request, database *db.DB) (string, bool) {
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return "", false
	}
	if !requireLLMProviderAdministrator(w, r, database, orgUUID) {
		return "", false
	}
	return orgUUID, true
}

func formatOrganizationLLMProvider(provider db.LLMProvider) llmProviderResponse {
	response := formatLLMProvider(provider)
	response.Scope = "organization"
	return response
}

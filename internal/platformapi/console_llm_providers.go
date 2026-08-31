package platformapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxLLMProviderModels = 100

type llmProviderRequest struct {
	Name     string   `json:"name"`
	BaseURL  string   `json:"base_url"`
	APIKey   *string  `json:"api_key"`
	ModelIDs []string `json:"model_ids"`
}

type llmProviderResponse struct {
	Type            string   `json:"type"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	BaseURL         string   `json:"base_url"`
	HasAPIKey       bool     `json:"has_api_key"`
	APIKeyLast4     string   `json:"api_key_last4"`
	ModelIDs        []string `json:"model_ids"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
	SkippedModelIDs []string `json:"skipped_model_ids,omitempty"`
}

func RegisterConsoleLLMProviderRoutes(r chi.Router, database *db.DB, secretService *secrets.Service) {
	client := llmproviders.NewHTTPClient(15 * time.Second)
	r.Route("/workspaces/{workspaceId}/llm_providers", func(r chi.Router) {
		r.Get("/", listConsoleLLMProviders(database))
		r.Post("/", createConsoleLLMProvider(database, secretService))
		r.Post("/preview_models", previewConsoleLLMProviderModels(database, client))
		r.Put("/{providerId}", updateConsoleLLMProvider(database, secretService))
		r.Delete("/{providerId}", deleteConsoleLLMProvider(database))
		r.Post("/{providerId}/models/sync", syncConsoleLLMProviderModels(database, secretService, client))
	})
}

func listConsoleLLMProviders(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, scope, ok := llmProviderScope(w, r, database)
		if !ok {
			return
		}
		providers, err := database.ListLLMProviders(r.Context(), orgUUID, scope.UUID)
		if err != nil {
			internalError(w, "failed to list LLM providers")
			return
		}
		response := make([]llmProviderResponse, 0, len(providers))
		for _, provider := range providers {
			response = append(response, formatLLMProvider(provider))
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func createConsoleLLMProvider(database *db.DB, secretService *secrets.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, scope, ok := llmProviderScope(w, r, database)
		if !ok {
			return
		}
		body, ok := decodeLLMProviderRequest(w, r)
		if !ok {
			return
		}
		if body.APIKey == nil || strings.TrimSpace(*body.APIKey) == "" {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeAPIKeyRequired, "api_key is required")
			return
		}
		apiKey := strings.TrimSpace(*body.APIKey)
		name, baseURL, modelIDs, ok := validateLLMProviderInput(w, body)
		if !ok {
			return
		}
		externalID, err := ids.New("llmprov_")
		if err != nil {
			internalError(w, "failed to create LLM provider")
			return
		}
		now := time.Now().UTC()
		provider := db.LLMProvider{
			UUID:             uuid.NewString(),
			ExternalID:       externalID,
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    scope.UUID,
			Name:             name,
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
		if writeLLMProviderModelConflictError(w, err) {
			return
		}
		if errors.Is(err, db.ErrDuplicate) {
			writeLLMProviderError(w, http.StatusConflict, llmProviderCodeNameConflict, "provider name already exists")
			return
		}
		if err != nil {
			internalError(w, "failed to create LLM provider")
			return
		}
		writeJSON(w, http.StatusCreated, formatLLMProvider(created))
	}
}

func updateConsoleLLMProvider(database *db.DB, secretService *secrets.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, scope, ok := llmProviderScope(w, r, database)
		if !ok {
			return
		}
		externalID := strings.TrimSpace(chi.URLParam(r, "providerId"))
		current, err := database.GetLLMProvider(r.Context(), orgUUID, scope.UUID, externalID)
		if errors.Is(err, db.ErrNotFound) {
			writeLLMProviderError(w, http.StatusNotFound, llmProviderCodeNotFound, "LLM provider not found")
			return
		}
		if err != nil {
			internalError(w, "failed to load LLM provider")
			return
		}
		body, ok := decodeLLMProviderRequest(w, r)
		if !ok {
			return
		}
		name, baseURL, modelIDs, ok := validateLLMProviderInput(w, body)
		if !ok {
			return
		}
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
		if writeLLMProviderModelConflictError(w, err) {
			return
		}
		if errors.Is(err, db.ErrDuplicate) {
			writeLLMProviderError(w, http.StatusConflict, llmProviderCodeNameConflict, "provider name already exists")
			return
		}
		if err != nil {
			internalError(w, "failed to update LLM provider")
			return
		}
		writeJSON(w, http.StatusOK, formatLLMProvider(updated))
	}
}

func deleteConsoleLLMProvider(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, scope, ok := llmProviderScope(w, r, database)
		if !ok {
			return
		}
		err := database.DeleteLLMProvider(
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
			internalError(w, "failed to delete LLM provider")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func llmProviderScope(w http.ResponseWriter, r *http.Request, database *db.DB) (string, WorkspaceScope, bool) {
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return "", WorkspaceScope{}, false
	}
	scope, ok := consoleWorkspaceScopeFromRequest(w, r, database, orgUUID)
	if !ok || !requireLLMProviderAdministrator(w, r, database, orgUUID) {
		return "", WorkspaceScope{}, false
	}
	return orgUUID, scope, true
}

func requireLLMProviderAdministrator(w http.ResponseWriter, r *http.Request, database *db.DB, orgUUID string) bool {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserExternalID == "" {
		writeLLMProviderPermissionDenied(w)
		return false
	}
	user, err := database.GetAdminUser(r.Context(), orgUUID, principal.UserExternalID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && !strings.EqualFold(user.Role, "admin")) {
		writeLLMProviderPermissionDenied(w)
		return false
	}
	if err != nil {
		internalError(w, "failed to authorize LLM provider management")
		return false
	}
	return true
}

func decodeLLMProviderRequest(w http.ResponseWriter, r *http.Request) (llmProviderRequest, bool) {
	body, err := readRequiredJSON[llmProviderRequest](r, true)
	if err != nil {
		writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeConfigurationInvalid, "request body must match LLM provider configuration")
		return llmProviderRequest{}, false
	}
	return body, true
}

func validateLLMProviderInput(w http.ResponseWriter, body llmProviderRequest) (string, string, []string, bool) {
	name := strings.TrimSpace(body.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeNameInvalid, "name must contain 1 to 100 characters")
		return "", "", nil, false
	}
	baseURL, err := llmproviders.ValidateBaseURL(body.BaseURL)
	if err != nil {
		code := llmProviderCodeBaseURLInvalid
		if errors.Is(err, llmproviders.ErrUnsafeBaseURL) {
			code = llmProviderCodeBaseURLUnsafe
		}
		writeLLMProviderError(w, http.StatusBadRequest, code, err.Error())
		return "", "", nil, false
	}
	modelIDs, err := normalizeLLMModelIDs(body.ModelIDs)
	if err != nil {
		inputErr, ok := errors.AsType[*llmProviderInputError](err)
		if !ok {
			internalError(w, "failed to validate LLM provider models")
			return "", "", nil, false
		}
		writeLLMProviderError(w, http.StatusBadRequest, inputErr.code, inputErr.Error())
		return "", "", nil, false
	}
	return name, baseURL, modelIDs, true
}

func normalizeLLMModelIDs(values []string) ([]string, error) {
	if len(values) > maxLLMProviderModels {
		return nil, newLLMProviderInputError(llmProviderCodeModelIDsLimit, "model_ids must contain at most 100 model IDs")
	}
	seen := make(map[string]struct{}, len(values))
	modelIDs := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > 255 {
			return nil, newLLMProviderInputError(llmProviderCodeModelIDInvalid, "each model_id must contain 1 to 255 characters")
		}
		if _, exists := seen[value]; exists {
			return nil, newLLMProviderInputError(llmProviderCodeModelIDsDuplicate, "model_ids must not contain duplicates")
		}
		seen[value] = struct{}{}
		modelIDs = append(modelIDs, value)
	}
	return modelIDs, nil
}

func configuredLLMModelIDs(
	ctx context.Context,
	database *db.DB,
	organizationUUID, workspaceUUID, excludedProviderID string,
) (map[string]struct{}, error) {
	providers, err := database.ListLLMProviders(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]struct{})
	for _, provider := range providers {
		if provider.ExternalID == excludedProviderID {
			continue
		}
		for _, modelID := range provider.ModelIDs {
			configured[modelID] = struct{}{}
		}
	}
	return configured, nil
}

func sealLLMProviderAPIKey(w http.ResponseWriter, r *http.Request, secretService *secrets.Service, provider *db.LLMProvider, apiKey string) bool {
	if secretService == nil {
		internalError(w, "LLM provider encryption is unavailable")
		return false
	}
	plaintext := []byte(apiKey)
	defer clear(plaintext)
	envelope, err := secretService.Seal(r.Context(), llmproviders.SecretBinding(*provider), plaintext)
	if err != nil {
		internalError(w, "failed to encrypt LLM provider API key")
		return false
	}
	provider.SecretEnvelope = &envelope
	return true
}

func formatLLMProvider(provider db.LLMProvider) llmProviderResponse {
	return llmProviderResponse{
		Type:        "llm_provider",
		ID:          provider.ExternalID,
		Name:        provider.Name,
		BaseURL:     provider.BaseURL,
		HasAPIKey:   true,
		APIKeyLast4: provider.APIKeyLast4,
		ModelIDs:    provider.ModelIDs,
		CreatedAt:   provider.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   provider.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func lastFour(value string) string {
	runes := []rune(value)
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

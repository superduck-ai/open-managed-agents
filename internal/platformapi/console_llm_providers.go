package platformapi

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"

	"github.com/go-chi/chi/v5"
)

const maxLLMProviderModels = 100

type llmProviderRequest struct {
	Models     db.LLMModels `json:"models"`
	APIBackend *string      `json:"api_backend"`
	Name       string       `json:"name"`
	Slug       *string      `json:"slug"`
	BaseURL    string       `json:"base_url"`
	APIKey     *string      `json:"api_key"`
	ModelIDs   []string     `json:"model_ids"`
}

type llmProviderResponse struct {
	Models      db.LLMModels `json:"models"`
	APIBackend  string       `json:"api_backend"`
	Type        string       `json:"type"`
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Slug        string       `json:"slug"`
	BaseURL     string       `json:"base_url"`
	HasAPIKey   bool         `json:"has_api_key"`
	APIKeyLast4 string       `json:"api_key_last4"`
	ModelIDs    []string     `json:"model_ids"`
	CreatedAt   string       `json:"created_at"`
	UpdatedAt   string       `json:"updated_at"`
	Scope       string       `json:"scope,omitempty"`
}

func RegisterConsoleLLMProviderRoutes(r chi.Router, database *db.DB, secretService *secrets.Service, presetsFile string) {
	registerConsoleLLMProviderPresets(r, database, presetsFile)
	client := llmproviders.NewHTTPClient(15 * time.Second)
	r.Route("/llm_provider", func(r chi.Router) {
		r.Get("/", getConsoleOrganizationLLMProvider(database))
		r.Put("/", putConsoleOrganizationLLMProvider(database, secretService))
		r.Delete("/", deleteConsoleOrganizationLLMProvider(database))
		r.Post("/preview_models", previewConsoleOrganizationLLMProviderModels(database, client))
		r.Post("/models/sync", syncConsoleOrganizationLLMProviderModels(database, secretService, client, true))
		r.Post("/models/preview", syncConsoleOrganizationLLMProviderModels(database, secretService, client, false))
	})
	r.Route("/llm_providers", func(r chi.Router) {
		r.Get("/", listConsoleLLMProviders(database))
		r.Post("/", createConsoleLLMProvider(database, secretService))
		r.Post("/preview_models", previewConsoleLLMProviderModels(database, client))
		r.Put("/{providerId}", updateConsoleLLMProvider(database, secretService))
		r.Delete("/{providerId}", deleteConsoleLLMProvider(database))
		r.Post("/{providerId}/models/sync", syncConsoleLLMProviderModels(database, secretService, client, true))
		r.Post("/{providerId}/models/preview", syncConsoleLLMProviderModels(database, secretService, client, false))
	})
	r.Route("/workspaces/{workspaceId}/llm_providers", func(r chi.Router) {
		r.Get("/", listConsoleLLMProviders(database))
		r.Post("/", createConsoleLLMProvider(database, secretService))
		r.Post("/preview_models", previewConsoleLLMProviderModels(database, client))
		r.Put("/{providerId}", updateConsoleLLMProvider(database, secretService))
		r.Delete("/{providerId}", deleteConsoleLLMProvider(database))
		r.Post("/{providerId}/models/sync", syncConsoleLLMProviderModels(database, secretService, client, true))
		r.Post("/{providerId}/models/preview", syncConsoleLLMProviderModels(database, secretService, client, false))
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
		slug, ok := newLLMProviderSlug(w, r, database, orgUUID, body.Slug)
		if !ok {
			return
		}
		name, baseURL, models, ok := validateLLMProviderInput(w, body, slug)
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
			UUID:             uuid.New().String(),
			ExternalID:       externalID,
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    scope.UUID,
			APIBackend:       llmProviderBackend(body.APIBackend, llmproviders.Messages),
			Name:             name,
			Slug:             slug,
			BaseURL:          baseURL,
			APIKeyLast4:      lastFour(apiKey),
			Models:           models,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if !sealLLMProviderAPIKey(w, r, secretService, &provider, apiKey) {
			return
		}
		created, err := database.CreateLLMProvider(r.Context(), provider)
		if errors.Is(err, db.ErrLLMProviderSlugConflict) {
			writeLLMProviderError(w, http.StatusConflict, llmProviderCodeSlugConflict, "provider slug already exists")
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
		name, baseURL, models, ok := validateLLMProviderInput(w, body, current.Slug)
		if !ok {
			return
		}
		if body.Slug != nil && strings.TrimSpace(*body.Slug) != "" && strings.TrimSpace(*body.Slug) != current.Slug {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeSlugImmutable, "slug cannot be changed after the provider is created")
			return
		}
		current.APIBackend = llmProviderBackend(body.APIBackend, current.APIBackend)
		current.Name = name
		current.BaseURL = baseURL
		if body.Models == nil {
			models = current.Models.WithIDs(body.ModelIDs)
		}
		current.Models = models
		current.UpdatedAt = time.Now().UTC()
		if body.APIKey != nil && strings.TrimSpace(*body.APIKey) != "" {
			apiKey := strings.TrimSpace(*body.APIKey)
			current.APIKeyLast4 = lastFour(apiKey)
			if !sealLLMProviderAPIKey(w, r, secretService, &current, apiKey) {
				return
			}
		}
		updated, err := database.UpdateLLMProvider(r.Context(), current)
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
	if chi.URLParam(r, "workspaceId") == "" {
		if r.Method != http.MethodGet && !requireLLMProviderAdministrator(w, r, database, orgUUID) {
			return "", WorkspaceScope{}, false
		}
		return orgUUID, WorkspaceScope{}, true
	}
	scope, ok := consoleWorkspaceScopeFromRequest(w, r, database, orgUUID)
	if !ok || (r.Method != http.MethodGet && !requireLLMProviderAdministrator(w, r, database, orgUUID)) {
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
	if body.Models != nil && body.ModelIDs != nil {
		writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeConfigurationInvalid, "use models or model_ids, not both")
		return llmProviderRequest{}, false
	}
	for index := range body.Models {
		model := &body.Models[index]
		model.Name = strings.TrimSpace(model.Name)
		if utf8.RuneCountInString(model.Name) > 100 {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeConfigurationInvalid, "model name must contain at most 100 characters")
			return llmProviderRequest{}, false
		}
		for _, value := range []*int{model.ContextWindow, model.MaxOutputTokens} {
			if value != nil && (*value <= 0 || *value > 9007199254740991) {
				writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeConfigurationInvalid, "model capacities must be positive safe integers")
				return llmProviderRequest{}, false
			}
		}
		if model.ImageInput != nil {
			image := model.ImageInput
			for _, value := range []int{image.MaxImages, image.MaxRequestImageBytes, image.MaxImageBytes, image.MaxLongEdge, image.MaxPixels, image.TokensPerMegapixel} {
				if value < 0 || int64(value) > llmproviders.MaxMessagesRequestBodyBytes {
					writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeConfigurationInvalid, "image input limits must be nonnegative integers within the proxy request limit")
					return llmProviderRequest{}, false
				}
			}
		}
		if !validModelModalities(model.InputModalities) {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeConfigurationInvalid, "model inputs must be a nonempty set of text/image")
			return llmProviderRequest{}, false
		}
	}
	return body, true
}

func validateLLMProviderInput(w http.ResponseWriter, body llmProviderRequest, slug string) (string, string, db.LLMModels, bool) {
	if body.APIBackend != nil && !llmproviders.ValidBackend(*body.APIBackend) {
		writeLLMProviderBackendError(w)
		return "", "", nil, false
	}
	name := cmp.Or(strings.TrimSpace(body.Name), slug)
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
	models := body.Models
	if models == nil {
		models = db.LLMModels{}.WithIDs(body.ModelIDs)
	}
	err = validateLLMModelIDs(models.IDs())
	if err != nil {
		inputErr, ok := errors.AsType[*llmProviderInputError](err)
		if !ok {
			internalError(w, "failed to validate LLM provider models")
			return "", "", nil, false
		}
		writeLLMProviderError(w, http.StatusBadRequest, inputErr.code, inputErr.Error())
		return "", "", nil, false
	}
	return name, baseURL, models, true
}

func validateLLMModelIDs(values []string) error {
	if len(values) > maxLLMProviderModels {
		return newLLMProviderInputError(llmProviderCodeModelIDsLimit, "model_ids must contain at most 100 model IDs")
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > 255 {
			return newLLMProviderInputError(llmProviderCodeModelIDInvalid, "each model_id must contain 1 to 255 characters")
		}
		if _, exists := seen[value]; exists {
			return newLLMProviderInputError(llmProviderCodeModelIDsDuplicate, "model_ids must not contain duplicates")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func newLLMProviderSlug(
	w http.ResponseWriter,
	r *http.Request,
	database *db.DB,
	organizationUUID string,
	requested *string,
) (string, bool) {
	if requested != nil && strings.TrimSpace(*requested) != "" {
		slug := strings.TrimSpace(*requested)
		if slug == llmproviders.OrganizationSlug {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeSlugInvalid, "provider slug is reserved for the organization default provider")
			return "", false
		}
		if err := llmproviders.ValidateSlug(slug); err != nil {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeSlugInvalid, err.Error())
			return "", false
		}
		return slug, true
	}
	providers, err := database.ListOrganizationLLMProviders(r.Context(), organizationUUID)
	if err != nil {
		internalError(w, "failed to list LLM providers")
		return "", false
	}
	taken := make([]string, 0, len(providers))
	for _, provider := range providers {
		taken = append(taken, provider.Slug)
	}
	return llmproviders.NextDefaultSlug(taken), true
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
	response := llmProviderResponse{
		APIBackend:  provider.APIBackend,
		Type:        "llm_provider",
		ID:          provider.ExternalID,
		Name:        provider.Name,
		Slug:        provider.Slug,
		BaseURL:     provider.BaseURL,
		HasAPIKey:   true,
		APIKeyLast4: provider.APIKeyLast4,
		Models:      provider.Models,
		ModelIDs:    provider.Models.IDs(),
		CreatedAt:   provider.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   provider.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if provider.WorkspaceUUID == "" {
		response.Scope = "organization"
	}
	return response
}

func lastFour(value string) string {
	runes := []rune(value)
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

func llmProviderBackend(value *string, fallback string) string {
	if value != nil {
		return *value
	}
	return fallback
}

func validModelModalities(values []string) bool {
	if values == nil {
		return true
	}
	if len(values) == 0 || len(values) > 2 {
		return false
	}
	for i, value := range values {
		if (value != "text" && value != "image") || slices.Contains(values[:i], value) {
			return false
		}
	}
	return true
}

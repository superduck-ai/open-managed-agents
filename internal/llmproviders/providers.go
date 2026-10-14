package llmproviders

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

var (
	ErrNotConfigured      = errors.New("workspace has no LLM provider configured")
	ErrModelNotConfigured = errors.New("model is not configured for this workspace")
	ErrAmbiguousModel     = errors.New("model is configured by more than one provider")
	ErrInvalidBaseURL     = errors.New("base_url must be an absolute HTTP or HTTPS URL")
	ErrUnsafeBaseURL      = errors.New("base_url must not contain credentials, query, or fragment")
)

const secretScope = "llm_provider"

type Upstream struct {
	BaseURL string
	APIKey  string
}

func Resolve(
	ctx context.Context,
	database *db.DB,
	secretService *secrets.Service,
	organizationUUID, workspaceUUID, modelID string,
) (Upstream, error) {
	if database == nil || secretService == nil {
		return Upstream{}, ErrNotConfigured
	}
	providers, organization, err := configuredProviders(ctx, database, organizationUUID, workspaceUUID)
	if err != nil {
		return Upstream{}, err
	}
	if len(providers) == 0 && organization == nil {
		return Upstream{}, ErrNotConfigured
	}
	selected, err := matchingWorkspaceProvider(providers, modelID)
	if err != nil {
		return Upstream{}, err
	}
	if selected == nil && organization != nil && containsModel(organization.ModelIDs, modelID) {
		selected = organization
	}
	if selected == nil {
		return Upstream{}, ErrModelNotConfigured
	}
	return openProviderUpstream(ctx, secretService, *selected)
}

func ListModelIDs(ctx context.Context, database *db.DB, organizationUUID, workspaceUUID string) ([]string, error) {
	if database == nil {
		return nil, ErrNotConfigured
	}
	providers, organization, err := configuredProviders(ctx, database, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 && organization == nil {
		return nil, ErrNotConfigured
	}
	models, err := workspaceModelIDs(providers)
	if err != nil {
		return nil, err
	}
	if organization == nil {
		return models, nil
	}
	seen := make(map[string]struct{}, len(models))
	for _, modelID := range models {
		seen[modelID] = struct{}{}
	}
	for _, modelID := range organization.ModelIDs {
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}
		models = append(models, modelID)
	}
	return models, nil
}

func configuredProviders(
	ctx context.Context,
	database *db.DB,
	organizationUUID, workspaceUUID string,
) ([]db.LLMProvider, *db.LLMProvider, error) {
	providers, err := database.ListLLMProviders(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, nil, err
	}
	organization, err := database.GetOrganizationLLMProvider(ctx, organizationUUID)
	if errors.Is(err, db.ErrNotFound) {
		return providers, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return providers, &organization, nil
}

func matchingWorkspaceProvider(providers []db.LLMProvider, modelID string) (*db.LLMProvider, error) {
	var selected *db.LLMProvider
	for index := range providers {
		if !containsModel(providers[index].ModelIDs, modelID) {
			continue
		}
		if selected != nil {
			return nil, ErrAmbiguousModel
		}
		selected = &providers[index]
	}
	return selected, nil
}

func workspaceModelIDs(providers []db.LLMProvider) ([]string, error) {
	models := make([]string, 0)
	seen := make(map[string]struct{})
	for _, provider := range providers {
		for _, modelID := range provider.ModelIDs {
			if _, exists := seen[modelID]; exists {
				return nil, ErrAmbiguousModel
			}
			seen[modelID] = struct{}{}
			models = append(models, modelID)
		}
	}
	return models, nil
}

func openProviderUpstream(ctx context.Context, secretService *secrets.Service, provider db.LLMProvider) (Upstream, error) {
	if provider.SecretEnvelope == nil {
		return Upstream{}, fmt.Errorf("open LLM provider API key: %w", db.ErrIncompleteLLMProviderSecret)
	}
	plaintext, err := secretService.Open(ctx, SecretBinding(provider), *provider.SecretEnvelope)
	if err != nil {
		return Upstream{}, fmt.Errorf("open LLM provider API key: %w", err)
	}
	defer clear(plaintext)
	return Upstream{BaseURL: provider.BaseURL, APIKey: string(plaintext)}, nil
}

func SecretBinding(provider db.LLMProvider) secrets.Binding {
	workspaceUUID := provider.WorkspaceUUID
	if workspaceUUID == "" {
		workspaceUUID = db.OrganizationLLMProviderSecretWorkspace
	}
	return secrets.Binding{
		OrganizationUUID:     provider.OrganizationUUID,
		WorkspaceUUID:        workspaceUUID,
		VaultExternalID:      secretScope,
		CredentialExternalID: provider.ExternalID,
	}
}

func ValidateBaseURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", ErrInvalidBaseURL
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrUnsafeBaseURL
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func Endpoint(baseURL, path, rawQuery string) (string, error) {
	validated, err := ValidateBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(validated)
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(path, "/")
	parsed.RawQuery = rawQuery
	return parsed.String(), nil
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport
	if base, ok := http.DefaultTransport.(*http.Transport); ok && base != nil {
		cloned := base.Clone()
		cloned.MaxIdleConnsPerHost = 32
		transport = cloned
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func containsModel(modelIDs []string, modelID string) bool {
	for _, configured := range modelIDs {
		if configured == modelID {
			return true
		}
	}
	return false
}

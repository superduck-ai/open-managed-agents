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
	providers, err := database.ListLLMProviders(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return Upstream{}, err
	}
	if len(providers) == 0 {
		return Upstream{}, ErrNotConfigured
	}
	var selected *db.LLMProvider
	for index := range providers {
		if !containsModel(providers[index].ModelIDs, modelID) {
			continue
		}
		if selected != nil {
			return Upstream{}, ErrAmbiguousModel
		}
		selected = &providers[index]
	}
	if selected == nil {
		return Upstream{}, ErrModelNotConfigured
	}
	plaintext, err := secretService.Open(ctx, SecretBinding(*selected), *selected.SecretEnvelope)
	if err != nil {
		return Upstream{}, fmt.Errorf("open LLM provider API key: %w", err)
	}
	defer clear(plaintext)
	return Upstream{BaseURL: selected.BaseURL, APIKey: string(plaintext)}, nil
}

func ListModelIDs(ctx context.Context, database *db.DB, organizationUUID, workspaceUUID string) ([]string, error) {
	if database == nil {
		return nil, ErrNotConfigured
	}
	providers, err := database.ListLLMProviders(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, ErrNotConfigured
	}
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

func SecretBinding(provider db.LLMProvider) secrets.Binding {
	return secrets.Binding{
		OrganizationUUID:     provider.OrganizationUUID,
		WorkspaceUUID:        provider.WorkspaceUUID,
		VaultExternalID:      secretScope,
		CredentialExternalID: provider.ExternalID,
	}
}

func ValidateBaseURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
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

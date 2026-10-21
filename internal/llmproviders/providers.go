package llmproviders

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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
	APIBackend       string
	BaseURL          string
	APIKey           string
	ModelID          string
	QualifiedModelID string
}

type AmbiguousModelError struct {
	ModelID    string
	Candidates []string
}

func (e *AmbiguousModelError) Error() string {
	return fmt.Sprintf(
		"model %q is configured by more than one provider; use one of: %s",
		e.ModelID,
		strings.Join(e.Candidates, ", "),
	)
}

func (e *AmbiguousModelError) Is(target error) bool {
	return target == ErrAmbiguousModel
}

type Catalog struct {
	providers    []db.LLMProvider
	organization *db.LLMProvider
}

type CatalogModel struct {
	Provider        db.LLMProvider
	UpstreamModelID string
}

func (m CatalogModel) QualifiedModelID() string {
	return QualifiedModelID(m.Provider.Slug, m.UpstreamModelID)
}

func NewCatalog(providers []db.LLMProvider) Catalog {
	return Catalog{providers: providers}
}

func LoadCatalog(ctx context.Context, database *db.DB, organizationUUID, workspaceUUID string) (Catalog, error) {
	if database == nil {
		return Catalog{}, ErrNotConfigured
	}
	providers, err := database.ListLLMProviders(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return Catalog{}, err
	}
	organization, err := database.GetOrganizationLLMProvider(ctx, organizationUUID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return Catalog{}, err
	}
	catalog := NewCatalog(providers)
	if err == nil {
		catalog.organization = &organization
	}
	if len(providers) == 0 && catalog.organization == nil {
		return Catalog{}, ErrNotConfigured
	}
	return catalog, nil
}

func (c Catalog) ModelIDs() []string {
	models := make([]string, 0)
	for _, provider := range c.providers {
		for _, modelID := range provider.ModelIDs {
			models = append(models, QualifiedModelID(provider.Slug, modelID))
		}
	}
	if c.organization != nil {
		for _, modelID := range c.organization.ModelIDs {
			models = append(models, QualifiedModelID(c.organization.Slug, modelID))
		}
	}
	return models
}

func (c Catalog) Lookup(modelID string) (CatalogModel, error) {
	if slug, upstreamModelID, ok := ParseModelRef(modelID); ok {
		for _, provider := range c.providers {
			if provider.Slug == slug && slices.Contains(provider.ModelIDs, upstreamModelID) {
				return CatalogModel{Provider: provider, UpstreamModelID: upstreamModelID}, nil
			}
		}
		if c.organization != nil && c.organization.Slug == slug && slices.Contains(c.organization.ModelIDs, upstreamModelID) {
			return CatalogModel{Provider: *c.organization, UpstreamModelID: upstreamModelID}, nil
		}
	}
	matches := make([]CatalogModel, 0, 1)
	for _, provider := range c.providers {
		if slices.Contains(provider.ModelIDs, modelID) {
			matches = append(matches, CatalogModel{Provider: provider, UpstreamModelID: modelID})
		}
	}
	switch len(matches) {
	case 0:
		if c.organization != nil && slices.Contains(c.organization.ModelIDs, modelID) {
			return CatalogModel{Provider: *c.organization, UpstreamModelID: modelID}, nil
		}
		return CatalogModel{}, ErrModelNotConfigured
	case 1:
		return matches[0], nil
	}
	candidates := make([]string, 0, len(matches))
	for _, match := range matches {
		candidates = append(candidates, match.QualifiedModelID())
	}
	return CatalogModel{}, &AmbiguousModelError{ModelID: modelID, Candidates: candidates}
}

func Resolve(
	ctx context.Context,
	database *db.DB,
	secretService *secrets.Service,
	organizationUUID, workspaceUUID, modelID string,
) (Upstream, error) {
	if secretService == nil {
		return Upstream{}, ErrNotConfigured
	}
	catalog, err := LoadCatalog(ctx, database, organizationUUID, workspaceUUID)
	if err != nil {
		return Upstream{}, err
	}
	selected, err := catalog.Lookup(modelID)
	if err != nil {
		return Upstream{}, err
	}
	provider := selected.Provider
	if provider.SecretEnvelope == nil {
		return Upstream{}, fmt.Errorf("open LLM provider API key: %w", db.ErrIncompleteLLMProviderSecret)
	}
	plaintext, err := secretService.Open(ctx, SecretBinding(provider), *provider.SecretEnvelope)
	if err != nil {
		return Upstream{}, fmt.Errorf("open LLM provider API key: %w", err)
	}
	defer clear(plaintext)
	return Upstream{
		APIBackend:       provider.APIBackend,
		BaseURL:          provider.BaseURL,
		APIKey:           string(plaintext),
		ModelID:          selected.UpstreamModelID,
		QualifiedModelID: selected.QualifiedModelID(),
	}, nil
}

func ListModelIDs(ctx context.Context, database *db.DB, organizationUUID, workspaceUUID string) ([]string, error) {
	catalog, err := LoadCatalog(ctx, database, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, err
	}
	return catalog.ModelIDs(), nil
}

func CanonicalModelID(ctx context.Context, database *db.DB, organizationUUID, workspaceUUID, modelID string) (string, error) {
	catalog, err := LoadCatalog(ctx, database, organizationUUID, workspaceUUID)
	if err != nil {
		return "", err
	}
	selected, err := catalog.Lookup(modelID)
	if err != nil {
		return "", err
	}
	return selected.QualifiedModelID(), nil
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

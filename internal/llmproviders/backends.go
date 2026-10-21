package llmproviders

import (
	"net/http"
	"slices"
)

const (
	Messages        = "messages"
	ChatCompletions = "chat_completions"
)

func ValidBackend(backend string) bool {
	return backend == Messages || backend == ChatCompletions
}

func (u Upstream) RequireBackend(backend string) error {
	actual := u.APIBackend
	if actual == "" {
		actual = Messages
	}
	if actual != backend {
		return ErrBackendMismatch
	}
	return nil
}

func (u Upstream) ApplyHeaders(headers http.Header) {
	headers.Del("Anthropic-Version")
	headers.Del("Anthropic-Beta")
	headers.Del("X-Api-Key")
	headers.Set("Authorization", "Bearer "+u.APIKey)
	if u.APIBackend != ChatCompletions {
		ApplyAPIKey(headers, u.APIKey)
		headers.Set("Anthropic-Version", anthropicVersion)
	}
}

func (c Catalog) Backends() (map[string]string, error) {
	providers := c.providers
	if c.organization != nil {
		providers = append(slices.Clone(providers), *c.organization)
	}
	backends := make(map[string]string)
	for _, provider := range providers {
		if !ValidBackend(provider.APIBackend) {
			return nil, ErrBackendMismatch
		}
		for _, modelID := range provider.ModelIDs {
			backends[QualifiedModelID(provider.Slug, modelID)] = provider.APIBackend
		}
	}
	for _, provider := range providers {
		for _, modelID := range provider.ModelIDs {
			if _, exists := backends[modelID]; exists {
				continue
			}
			selected, err := c.Lookup(modelID)
			if err == nil && selected.UpstreamModelID == modelID {
				backends[modelID] = selected.Provider.APIBackend
			}
		}
	}
	return backends, nil
}

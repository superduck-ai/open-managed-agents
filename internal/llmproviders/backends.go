package llmproviders

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
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

func (c Catalog) modelSelections() map[string]CatalogModel {
	ids := c.ModelIDs()
	for _, id := range slices.Clone(ids) {
		if selected, err := c.Lookup(id); err == nil {
			ids = append(ids, selected.UpstreamModelID)
		}
	}
	selectedModels := make(map[string]CatalogModel)
	for _, id := range ids {
		if selected, err := c.Lookup(id); err == nil {
			selectedModels[id] = selected
		}
	}
	return selectedModels
}

func (c Catalog) ContextWindows() map[string]int {
	windows := make(map[string]int)
	for id, selected := range c.modelSelections() {
		if value := selected.Provider.ModelContextWindows[selected.UpstreamModelID]; value > 0 {
			windows[id] = value
		}
	}
	return windows
}

func (c Catalog) ModelOptions() map[string]db.ModelOptions {
	options := make(map[string]db.ModelOptions)
	for id, selected := range c.modelSelections() {
		if value, ok := selected.Provider.ModelOptions[selected.UpstreamModelID]; ok {
			options[id] = value
		}
	}
	return options
}

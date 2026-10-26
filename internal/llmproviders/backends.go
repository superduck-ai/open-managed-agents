package llmproviders

import (
	"net/http"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/db"
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

type RuntimeModel struct {
	db.LLMModel
	APIBackend string `json:"api_backend"`
}

func (c Catalog) RuntimeModels() (map[string]RuntimeModel, error) {
	providers := c.providers
	if c.organization != nil {
		providers = append(slices.Clone(providers), *c.organization)
	}
	for _, provider := range providers {
		if !ValidBackend(provider.APIBackend) {
			return nil, ErrBackendMismatch
		}
	}
	ids := c.ModelIDs()
	for _, id := range slices.Clone(ids) {
		if selected, err := c.Lookup(id); err == nil {
			ids = append(ids, selected.UpstreamModelID)
		}
	}
	models := make(map[string]RuntimeModel)
	for _, id := range ids {
		selected, err := c.Lookup(id)
		if err != nil {
			continue
		}
		index := slices.IndexFunc(selected.Provider.Models, func(model db.LLMModel) bool { return model.ID == selected.UpstreamModelID })
		models[id] = RuntimeModel{LLMModel: selected.Provider.Models[index], APIBackend: selected.Provider.APIBackend}
	}
	return models, nil
}

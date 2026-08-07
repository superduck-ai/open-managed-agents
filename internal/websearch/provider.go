package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

type Provider interface {
	Search(context.Context, SearchRequest) (SearchResponse, error)
	ValidateOptions(SearchOptions) error
}

type providerFactory interface {
	New(config.WebSearchProviderConfig, time.Duration, *http.Client) (Provider, error)
}

// builtInProviderFactories is deliberately static: adding a provider requires an
// explicit, reviewable change here, and duplicate names fail at compile time.
var builtInProviderFactories = map[string]providerFactory{
	"brave":  braveFactory{},
	"tavily": tavilyFactory{},
}

func NewProvider(cfg config.WebSearchConfig, client *http.Client) (Provider, error) {
	name := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if name == "" {
		return nil, nil
	}
	factory, ok := builtInProviderFactories[name]
	if !ok {
		return nil, fmt.Errorf("web search provider %q is unsupported", cfg.Provider)
	}
	providerConfig, ok := configuredProvider(cfg.Providers, name)
	if !ok {
		return nil, fmt.Errorf("web search provider %q has no configuration", cfg.Provider)
	}
	if strings.TrimSpace(providerConfig.APIKey) == "" {
		return nil, nil
	}
	return factory.New(providerConfig, cfg.Timeout, client)
}

func configuredProvider(providers map[string]config.WebSearchProviderConfig, name string) (config.WebSearchProviderConfig, bool) {
	for configuredName, provider := range providers {
		if strings.ToLower(strings.TrimSpace(configuredName)) == name {
			return provider, true
		}
	}
	return config.WebSearchProviderConfig{}, false
}

func decodeProviderOptions(raw map[string]json.RawMessage, target any) error {
	if len(raw) == 0 {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("encode provider options: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode provider options: %w", err)
	}
	return nil
}

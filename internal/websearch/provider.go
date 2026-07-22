package websearch

import (
	"context"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

type Provider interface {
	Search(context.Context, SearchRequest) (SearchResponse, error)
}

func NewProvider(cfg config.WebSearchConfig, client *http.Client) Provider {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "tavily":
		if strings.TrimSpace(cfg.APIKey) == "" {
			return nil
		}
		return NewTavilyClient(cfg.Endpoint, cfg.APIKey, cfg.Timeout, client)
	case "brave":
		if strings.TrimSpace(cfg.APIKey) == "" {
			return nil
		}
		return NewBraveClient(BraveClientConfig{
			Endpoint: cfg.Endpoint,
			APIKey:   cfg.APIKey,
			Timeout:  cfg.Timeout,
			Options: SearchOptions{
				Country:        cfg.Brave.Country,
				SearchLanguage: cfg.Brave.SearchLanguage,
				UILanguage:     cfg.Brave.UILanguage,
				Freshness:      cfg.Brave.Freshness,
				SafeSearch:     cfg.Brave.SafeSearch,
				Spellcheck:     cfg.Brave.Spellcheck,
				ResultFilter:   cfg.Brave.ResultFilter,
				Goggles:        append([]string(nil), cfg.Brave.Goggles...),
				ExtraSnippets:  cfg.Brave.ExtraSnippets,
				Units:          cfg.Brave.Units,
			},
		}, client)
	default:
		return nil
	}
}

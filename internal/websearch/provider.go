package websearch

import (
	"context"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

// Provider searches the web.
type Provider interface {
	Search(context.Context, string, SearchOptions) ([]Result, error)
}

// NewProvider returns the configured provider, or nil when disabled.
func NewProvider(cfg config.WebSearchConfig, client *http.Client) Provider {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "tavily":
		if strings.TrimSpace(cfg.APIKey) == "" {
			return nil
		}
		return NewTavilyClient(cfg.Endpoint, cfg.APIKey, cfg.Timeout, client)
	default:
		return nil
	}
}

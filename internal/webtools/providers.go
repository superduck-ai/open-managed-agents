package webtools

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func Load(cfg config.WebToolsConfig) (*Client, error) {
	provider := strings.TrimSpace(cfg.SearchProvider)
	if provider == "" {
		if len(cfg.Providers) != 0 {
			return nil, invalidConfig("web_tools.search_provider is required when providers are configured")
		}
		return NewClient(nil, nil), nil
	}
	switch provider {
	case "deepseek":
		key := strings.TrimSpace(cfg.Providers[provider].APIKey)
		if key == "" || strings.ContainsAny(key, "\r\n\t ") {
			return nil, invalidConfig("web_tools.providers.deepseek.api_key must be nonempty and contain no whitespace")
		}
		return NewClient(NewDeepSeekSearch(key, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}), nil), nil
	default:
		return nil, invalidConfig("web_tools.search_provider is unsupported")
	}
}

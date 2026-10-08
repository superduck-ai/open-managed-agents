package tunnels

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func canonicalTunnelMCPURL(r *http.Request, cfg config.TunnelConfig, tunnelID, channel string) string {
	baseURL := strings.TrimRight(cfg.PublicBaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(httpapi.RequestBaseURL(r), "/")
	}
	path := "/v1/mcp/" + url.PathEscape(tunnelID)
	if channel != "" && channel != "main" {
		path += "/" + url.PathEscape(channel)
	}
	return baseURL + path
}

func canonicalTunnelOAuthMetadataURL(r *http.Request, cfg config.TunnelConfig, tunnelID, channel string) string {
	baseURL := strings.TrimRight(cfg.PublicBaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(httpapi.RequestBaseURL(r), "/")
	}
	path := "/.well-known/oauth-protected-resource/v1/mcp/" + url.PathEscape(tunnelID)
	if channel != "" && channel != "main" {
		path += "/" + url.PathEscape(channel)
	}
	return baseURL + path
}

func tunnelTargetUsesPublicOrigin(target *url.URL, cfg config.TunnelConfig) bool {
	publicBaseURL := cfg.PublicBaseURL
	if target == nil || publicBaseURL == "" {
		return false
	}
	configured, err := url.Parse(publicBaseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(target.Scheme, configured.Scheme) && strings.EqualFold(target.Host, configured.Host)
}

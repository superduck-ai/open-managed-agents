package tunnels

import (
	"errors"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

// TargetReference is the lookup key and channel encoded by a canonical Tunnel
// MCP URL. Domain is set only for hostname-alias targets; otherwise TunnelID is
// set for the canonical /v1/mcp/{tunnel_id} form.
type TargetReference struct {
	TunnelID string
	Domain   string
	Channel  string
}

// RecognizeTarget classifies only URLs owned by the configured Tunnel data
// plane. A third-party URL with a matching path remains an ordinary MCP target.
// The recognized result is true for malformed URLs that claim a configured
// Tunnel origin so callers can fail closed instead of forwarding them elsewhere.
func RecognizeTarget(target *url.URL, cfg config.TunnelConfig) (TargetReference, bool, error) {
	if target == nil {
		return TargetReference{}, false, nil
	}
	segments := splitTunnelPath(target.Path)
	if len(segments) >= 2 && segments[0] == "v1" && segments[1] == "mcp" {
		return recognizeCanonicalTarget(target, cfg, segments)
	}
	return recognizeHostnameTarget(target, cfg, segments)
}

func recognizeCanonicalTarget(
	target *url.URL,
	cfg config.TunnelConfig,
	segments []string,
) (TargetReference, bool, error) {
	if !tunnelTargetUsesPublicOrigin(target, cfg) {
		return TargetReference{}, false, nil
	}
	if target.RawQuery != "" || target.Fragment != "" {
		return TargetReference{}, true, errors.New("tunnel MCP URL must not include a query or fragment")
	}
	if len(segments) < 3 || len(segments) > 4 {
		return TargetReference{}, true, errors.New("tunnel MCP URL path is invalid")
	}
	if !tunnelIDPattern.MatchString(segments[2]) {
		return TargetReference{}, true, errors.New("tunnel MCP URL tunnel ID is invalid")
	}
	channel := "main"
	if len(segments) == 4 {
		channel = segments[3]
	}
	if !channelNamePattern.MatchString(channel) {
		return TargetReference{}, true, errors.New("tunnel MCP URL channel is invalid")
	}
	return TargetReference{TunnelID: segments[2], Channel: channel}, true, nil
}

func recognizeHostnameTarget(
	target *url.URL,
	cfg config.TunnelConfig,
	segments []string,
) (TargetReference, bool, error) {
	host := strings.ToLower(target.Hostname())
	if host == "" || !strings.HasSuffix(host, "."+cfg.DomainSuffix) {
		return TargetReference{}, false, nil
	}
	if len(segments) > 1 {
		return TargetReference{}, true, errors.New("tunnel hostname URL path is invalid")
	}
	if target.RawQuery != "" || target.Fragment != "" {
		return TargetReference{}, true, errors.New("tunnel hostname URL must not include a query or fragment")
	}
	channel := "main"
	if len(segments) == 1 {
		channel = segments[0]
	}
	if !channelNamePattern.MatchString(channel) {
		return TargetReference{}, true, errors.New("tunnel hostname URL channel is invalid")
	}
	return TargetReference{Domain: host, Channel: channel}, true, nil
}

func splitTunnelPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

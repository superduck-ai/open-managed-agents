package networkpolicy

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func ValidateSessionMCPHosts(configRaw, agentSnapshotRaw json.RawMessage) error {
	config, err := ParseConfig(configRaw)
	if err != nil {
		return err
	}
	if config.Type != TypeLimited || config.AllowMCPServers {
		return nil
	}
	targets, err := parseMCPServerTargets(agentSnapshotRaw)
	if err != nil {
		return err
	}
	matcher := newHostMatcher(config.allowedHosts)
	for _, rawURL := range targets.urls {
		target, err := url.Parse(rawURL)
		if err != nil {
			return ErrMalformedAgentSnapshot
		}
		host, err := NormalizeHost(target.Hostname())
		if err != nil {
			return ErrMalformedAgentSnapshot
		}
		port := target.Port()
		if port == "" {
			port = "443"
			if strings.EqualFold(target.Scheme, "http") {
				port = "80"
			}
		}
		if !matcher.match(host, port) {
			return fmt.Errorf("agent MCP server host %q is not allowed by environment networking", host)
		}
	}
	return nil
}

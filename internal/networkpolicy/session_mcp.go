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
	urls, err := sessionMCPServerURLs(agentSnapshotRaw)
	if err != nil {
		return err
	}
	matcher := newHostMatcher(config.allowedHosts)
	for _, rawURL := range urls {
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

type sessionMCPAgent struct {
	MCPServers mcpServerListSchema   `json:"mcp_servers"`
	Multiagent *sessionMCPMultiagent `json:"multiagent"`
}
type sessionMCPMultiagent struct {
	Subagents sessionMCPDelegation `json:"subagents"`
	Workflows sessionMCPDelegation `json:"workflows"`
}
type sessionMCPDelegation struct {
	Type             string             `json:"type"`
	PredefinedAgents []*sessionMCPAgent `json:"predefined_agents"`
}

func sessionMCPServerURLs(raw json.RawMessage) ([]string, error) {
	targets, err := parseMCPServerTargets(raw)
	if err != nil {
		return nil, err
	}
	var snapshot sessionMCPAgent
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("%w: invalid predefined MCP snapshot", ErrMalformedAgentSnapshot)
	}
	urls := targets.urls
	pending := []*sessionMCPMultiagent{snapshot.Multiagent}
	for len(pending) > 0 {
		multi := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if multi == nil {
			continue
		}
		for _, delegation := range []sessionMCPDelegation{multi.Subagents, multi.Workflows} {
			if delegation.Type != "enabled" {
				continue
			}
			for _, agent := range delegation.PredefinedAgents {
				if agent == nil {
					return nil, ErrMalformedAgentSnapshot
				}
				if len(agent.MCPServers) > 0 {
					encoded, err := json.Marshal(agentSnapshotSchema{MCPServers: agent.MCPServers})
					if err != nil {
						return nil, fmt.Errorf("%w: encode predefined MCP snapshot", ErrMalformedAgentSnapshot)
					}
					child, err := parseMCPServerTargets(encoded)
					if err != nil {
						return nil, err
					}
					urls = append(urls, child.urls...)
				}
				pending = append(pending, agent.Multiagent)
			}
		}
	}
	return urls, nil
}

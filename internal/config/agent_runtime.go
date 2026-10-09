package config

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

func validateAgentRuntimeConfig(cfg EnvironmentRunnerConfig) error {
	if cfg.AgentMode != "" && cfg.AgentMode != "sandbox" && cfg.AgentMode != "host" {
		return errors.New("environment_runner.agent_mode must be sandbox or host")
	}
	if cfg.SandboxMCPPort < 0 || cfg.SandboxMCPPort > 65535 {
		return errors.New("environment_runner.sandbox_mcp_port must be between 1 and 65535")
	}
	if cfg.SandboxMCPPath == "" {
		return nil
	}
	u, err := url.Parse(cfg.SandboxMCPPath)
	if err != nil || u.Path == "" || u.Path[0] != '/' || u.Host != "" || u.IsAbs() || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("environment_runner.sandbox_mcp_path must be an absolute URL path without query or fragment")
	}
	return nil
}

func validateLocalSandboxEndpointConfig(cfg E2BConfig) error {
	if cfg.LocalPortLookup {
		u, err := url.Parse(cfg.APIURL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" {
			return errors.New("e2b.local_port_lookup requires an HTTP API URL without credentials, query or fragment")
		}
	}
	if cfg.LocalServiceHost == "" {
		return nil
	}
	if !cfg.LocalPortLookup {
		return errors.New("e2b.local_service_host requires local_port_lookup")
	}
	if net.ParseIP(cfg.LocalServiceHost) != nil {
		return nil
	}
	for _, label := range strings.Split(cfg.LocalServiceHost, ".") {
		if !validServiceHostLabel(label) {
			return errors.New("e2b.local_service_host must be a hostname or IP address without a port")
		}
	}
	return nil
}

func validServiceHostLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, ch := range label {
		if ch != '-' && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

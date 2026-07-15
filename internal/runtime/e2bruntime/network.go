package e2bruntime

import (
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

func resolveNetwork(raw json.RawMessage, mcpAllowedHosts []string) (*e2b.SandboxNetworkOpts, bool, error) {
	var config struct {
		Type       string `json:"type"`
		Networking *struct {
			Type                 string   `json:"type"`
			AllowedHosts         []string `json:"allowed_hosts"`
			AllowPackageManagers bool     `json:"allow_package_managers"`
			AllowMCPServers      bool     `json:"allow_mcp_servers"`
		} `json:"networking"`
	}
	if len(raw) == 0 {
		return nil, true, nil
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, false, err
	}
	if config.Type != "cloud" || config.Networking == nil || config.Networking.Type == "unrestricted" {
		return nil, true, nil
	}
	if config.Networking.Type != "limited" {
		return nil, false, nil
	}
	hosts := append([]string(nil), config.Networking.AllowedHosts...)
	if config.Networking.AllowPackageManagers {
		hosts = append(hosts, packageManagerHosts()...)
	}
	if config.Networking.AllowMCPServers {
		hosts = append(hosts, mcpAllowedHosts...)
	}
	return &e2b.SandboxNetworkOpts{AllowOut: uniqueStrings(hosts)}, false, nil
}

func mcpAllowedHostsFromWork(work *db.EnvironmentWork) []string {
	if work == nil || len(work.Metadata) == 0 || strings.TrimSpace(string(work.Metadata)) == "null" {
		return nil
	}
	var metadata map[string]any
	if err := json.Unmarshal(work.Metadata, &metadata); err != nil {
		return nil
	}
	values, ok := metadata["mcp_allowed_hosts"].([]any)
	if !ok {
		return nil
	}
	hosts := make([]string, 0, len(values))
	for _, value := range values {
		host, ok := value.(string)
		if !ok {
			continue
		}
		hosts = append(hosts, host)
	}
	return uniqueStrings(hosts)
}

func packageManagerHosts() []string {
	return []string{
		"archive.ubuntu.com",
		"security.ubuntu.com",
		"pypi.org",
		"files.pythonhosted.org",
		"registry.npmjs.org",
		"proxy.golang.org",
		"sum.golang.org",
		"crates.io",
		"index.crates.io",
		"rubygems.org",
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

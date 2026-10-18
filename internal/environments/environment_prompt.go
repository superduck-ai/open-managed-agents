package environments

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

const (
	maxPromptAllowedHosts       = 50
	maxPromptPackagesPerManager = 20
)

//go:embed environment_prompt.tmpl
var environmentPromptSource string

var environmentPromptTemplate = template.Must(template.New("environment").Parse(environmentPromptSource))

type environmentPromptData struct {
	Unrestricted bool
	Reachable    string
	Packages     string
}

func managedAgentEnvironmentPrompt(environmentConfig json.RawMessage) (string, error) {
	policy, err := networkpolicy.ParseConfig(environmentConfig)
	if err != nil {
		return "", fmt.Errorf("parse environment networking: %w", err)
	}
	data := environmentPromptData{
		Unrestricted: policy.Type != networkpolicy.TypeLimited,
		Reachable:    reachableDestinations(policy),
	}
	if len(environmentConfig) > 0 {
		packages, err := decodeEnvironmentPackages(environmentConfig)
		if err != nil {
			return "", fmt.Errorf("parse environment packages: %w", err)
		}
		data.Packages = packagesSummary(packages)
	}
	var prompt strings.Builder
	if err := environmentPromptTemplate.Execute(&prompt, data); err != nil {
		return "", fmt.Errorf("render environment prompt: %w", err)
	}
	return strings.TrimRight(prompt.String(), "\n"), nil
}

func reachableDestinations(policy networkpolicy.Config) string {
	var destinations []string
	if policy.AllowPackageManagers {
		destinations = append(destinations, "package registries")
	}
	if policy.AllowMCPServers {
		destinations = append(destinations, "configured MCP servers")
	}
	if hosts := policy.AllowedHostPatterns(); len(hosts) > 0 {
		destinations = append(destinations, "these hosts: "+truncatedList(hosts, maxPromptAllowedHosts))
	}
	switch len(destinations) {
	case 0:
		return ""
	case 1:
		return destinations[0]
	default:
		return strings.Join(destinations[:len(destinations)-1], ", ") + " and " + destinations[len(destinations)-1]
	}
}

func packagesSummary(packages *environmentPackages) string {
	if packages == nil {
		return ""
	}
	var groups []string
	for _, manager := range packages.specsByManager() {
		if len(*manager.specs) > 0 {
			groups = append(groups, manager.manager+": "+truncatedList(*manager.specs, maxPromptPackagesPerManager))
		}
	}
	return strings.Join(groups, "; ")
}

func truncatedList(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(values[:limit], ", "), len(values)-limit)
}

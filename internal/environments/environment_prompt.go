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
	Unrestricted         bool
	AllowedHosts         string
	AllowPackageManagers bool
	AllowMCPServers      bool
	Packages             string
}

func managedAgentEnvironmentPrompt(environmentConfig json.RawMessage) (string, error) {
	policy, err := networkpolicy.ParseConfig(environmentConfig)
	if err != nil {
		return "", fmt.Errorf("parse environment networking: %w", err)
	}
	data := environmentPromptData{
		Unrestricted:         policy.Type != networkpolicy.TypeLimited,
		AllowedHosts:         truncatedList(policy.AllowedHostPatterns(), maxPromptAllowedHosts),
		AllowPackageManagers: policy.AllowPackageManagers,
		AllowMCPServers:      policy.AllowMCPServers,
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

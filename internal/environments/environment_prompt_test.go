package environments

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func mustEnvironmentPrompt(t *testing.T, environmentConfig json.RawMessage) string {
	t.Helper()
	prompt, err := managedAgentEnvironmentPrompt(environmentConfig, "")
	if err != nil {
		t.Fatal(err)
	}
	return prompt
}

func TestManagedAgentEnvironmentPromptRejectsInvalidConfig(t *testing.T) {
	for _, raw := range []string{
		`{`,
		`{"type":"cloud","networking":{"type":"sometimes"}}`,
		`{"type":"cloud","packages":{"type":"packages","pip":["--index-url=https://example.test"]}}`,
	} {
		if _, err := managedAgentEnvironmentPrompt(json.RawMessage(raw), ""); err == nil {
			t.Errorf("config %s accepted", raw)
		}
	}
}

func TestManagedAgentEnvironmentPromptDefaultsToUnrestricted(t *testing.T) {
	prompt := mustEnvironmentPrompt(t, nil)
	for _, want := range []string{
		"# Managed-agent environment",
		"/mnt/session/uploads | read-only",
		"never /mnt/session/uploads/uploads/<p>",
		"whether you are reading, reviewing or changing it",
		"write the modified copy to /mnt/user-data/outputs",
		"including a bare file name: check /mnt/session/uploads first",
		"ask whoever gave you the task which one to use",
		"/mnt/user-data/outputs | read-write",
		"## Persistence",
		"Outbound network access is unrestricted.",
		"You can install other software",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{"Preinstalled", "blocked by the environment's network policy", "{{", "\n\n\n"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("prompt contains %q:\n%s", unwanted, prompt)
		}
	}
	if prompt != mustEnvironmentPrompt(t, json.RawMessage(`{"type":"cloud"}`)) {
		t.Fatal("prompt differs between missing config and cloud defaults")
	}
}

func TestManagedAgentEnvironmentPromptSystemInformation(t *testing.T) {
	for _, test := range []struct {
		environment  string
		architecture string
	}{
		{environment: config.EnvironmentDev, architecture: "aarch64"},
		{environment: config.EnvironmentProd, architecture: "x86_64"},
		{architecture: "x86_64"},
	} {
		t.Run(test.environment+test.architecture, func(t *testing.T) {
			prompt, err := managedAgentEnvironmentPrompt(nil, test.environment)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"Operating system: Linux",
				"Distribution: Ubuntu 24.04.4 LTS (Noble Numbat)",
				"Architecture: " + test.architecture,
				"Shell: /bin/bash",
				"Current user: root",
				"user account exists with UID 1000 and belongs to the sudo group",
				"sudo is available at /usr/bin/sudo",
			} {
				if !strings.Contains(prompt, want) {
					t.Errorf("prompt missing %q:\n%s", want, prompt)
				}
			}
		})
	}
}

func TestManagedAgentEnvironmentPromptRendersLimitedNetworkAndPackages(t *testing.T) {
	prompt := mustEnvironmentPrompt(t, json.RawMessage(`{
		"type":"cloud",
		"networking":{"type":"limited","allowed_hosts":["api.example.com","*.example.org"],"allow_package_managers":true,"allow_mcp_servers":true},
		"packages":{"type":"packages","apt":["ripgrep"],"pip":["pandas==2.2.0","numpy"]}
	}`))
	for _, want := range []string{
		"Outbound network access is limited to package registries, configured MCP servers and these hosts: api.example.com, *.example.org. Requests to any other host are blocked",
		"Preinstalled for this environment: apt: ripgrep; pip: pandas==2.2.0, numpy.\nYou can install",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "unrestricted") {
		t.Fatalf("limited prompt claims unrestricted access:\n%s", prompt)
	}
}

func TestManagedAgentEnvironmentPromptBlocksWithoutAllowedHosts(t *testing.T) {
	prompt := mustEnvironmentPrompt(t, json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":[]}}`))
	if !strings.Contains(prompt, "All outbound network access is blocked by the environment's network policy") || strings.Contains(prompt, "limited to") {
		t.Fatalf("unexpected network section:\n%s", prompt)
	}
}

func TestManagedAgentEnvironmentPromptTruncatesLongLists(t *testing.T) {
	hosts := make([]string, maxPromptAllowedHosts+3)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("h%d.example.com", i)
	}
	encoded, err := json.Marshal(hosts)
	if err != nil {
		t.Fatal(err)
	}
	prompt := mustEnvironmentPrompt(t, json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":`+string(encoded)+`}}`))
	if !strings.Contains(prompt, "h49.example.com and 3 more.") || strings.Contains(prompt, "h50.example.com") {
		t.Fatalf("host list not truncated:\n%s", prompt)
	}
}

func TestManagedAgentEnvironmentPromptDescribesRegistriesWithoutAllowedHosts(t *testing.T) {
	for raw, want := range map[string]string{
		`{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_package_managers":true}}`:             "Outbound network access is limited to package registries. Requests to any other host are blocked",
		`{"type":"cloud","networking":{"type":"limited","allow_mcp_servers":true}}`:                                     "Outbound network access is limited to configured MCP servers. Requests to any other host are blocked",
		`{"type":"cloud","networking":{"type":"limited","allow_package_managers":true,"allow_mcp_servers":true}}`:       "Outbound network access is limited to package registries and configured MCP servers. Requests",
		`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true}}`: "Outbound network access is limited to configured MCP servers and these hosts: api.example.com. Requests",
	} {
		prompt := mustEnvironmentPrompt(t, json.RawMessage(raw))
		if !strings.Contains(prompt, want) || strings.Contains(prompt, "access is blocked") {
			t.Errorf("config %s rendered:\n%s", raw, prompt)
		}
	}
}

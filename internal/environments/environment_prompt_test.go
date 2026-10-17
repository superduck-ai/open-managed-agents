package environments

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func mustEnvironmentPrompt(t *testing.T, environmentConfig json.RawMessage) string {
	t.Helper()
	prompt, err := managedAgentEnvironmentPrompt(environmentConfig)
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
		if _, err := managedAgentEnvironmentPrompt(json.RawMessage(raw)); err == nil {
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

func TestManagedAgentEnvironmentPromptRendersLimitedNetworkAndPackages(t *testing.T) {
	prompt := mustEnvironmentPrompt(t, json.RawMessage(`{
		"type":"cloud",
		"networking":{"type":"limited","allowed_hosts":["api.example.com","*.example.org"],"allow_package_managers":true,"allow_mcp_servers":true},
		"packages":{"type":"packages","apt":["ripgrep"],"pip":["pandas==2.2.0","numpy"]}
	}`))
	for _, want := range []string{
		"Outbound network access is limited to these hosts: api.example.com, *.example.org. Package registries are also reachable. Configured MCP servers are also reachable.\nRequests to any other host are blocked",
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
	if !strings.Contains(prompt, "Outbound network access is blocked.\nRequests to any other host") || strings.Contains(prompt, "also reachable") {
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

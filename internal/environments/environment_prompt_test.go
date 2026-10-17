package environments

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

func TestEnvironmentPromptInputRejectsInvalidConfig(t *testing.T) {
	for name, raw := range map[string]string{
		"malformed":        `{`,
		"unknown network":  `{"type":"cloud","networking":{"type":"open"}}`,
		"invalid packages": `{"type":"cloud","packages":{"type":"packages","pip":["--index-url=x"]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := environmentPromptInputFromConfig(json.RawMessage(raw)); err == nil {
				t.Fatalf("config %s accepted", raw)
			}
		})
	}
	_, err := environmentPromptInputFromConfig(json.RawMessage(`{"type":"cloud","networking":{"type":"open"}}`))
	if !errors.Is(err, networkpolicy.ErrUnknownNetworkingType) {
		t.Fatalf("error = %v, want unknown networking type", err)
	}
}

func TestEnvironmentPromptDefaultsToUnrestrictedWithoutPackages(t *testing.T) {
	input, err := environmentPromptInputFromConfig(json.RawMessage(`{"type":"cloud"}`))
	if err != nil {
		t.Fatal(err)
	}
	prompt := renderEnvironmentPrompt(input)
	if prompt != renderEnvironmentPrompt(environmentPromptInput{}) {
		t.Fatalf("cloud default differs from zero input:\n%s", prompt)
	}
	for _, fragment := range []string{
		"# Environment\n\n",
		"- /mnt/session/uploads (read-only)",
		"- /mnt/user-data/outputs (read-write)",
		"## Persistence",
		"## Network\n\nOutbound network access is unrestricted.",
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("prompt missing %q:\n%s", fragment, prompt)
		}
	}
	if strings.Contains(prompt, "Preinstalled") || strings.Contains(prompt, "Git repository") {
		t.Fatalf("prompt mentions absent packages or repositories:\n%s", prompt)
	}
}

func TestEnvironmentPromptRendersLimitedNetworkPackagesAndRepositories(t *testing.T) {
	input, err := environmentPromptInputFromConfig(json.RawMessage(`{
		"type":"cloud",
		"networking":{"type":"limited","allowed_hosts":["api.example.com","*.internal.example"],"allow_package_managers":true,"allow_mcp_servers":true},
		"packages":{"type":"packages","apt":["ffmpeg"],"pip":["pandas==2.2.3","httpx"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	resources := managedAgentRuntimeResources{
		environment:  input,
		repositories: []repositoryMount{{repo: "acme/api", mountPath: "/workspace/api"}},
	}
	prompt := resources.environmentPrompt()
	for _, fragment := range []string{
		"- /workspace/api: Git repository acme/api.",
		"limited to these hosts: api.example.com, *.internal.example.",
		"Package registries are also reachable.",
		"The configured MCP servers are also reachable.",
		"do not retry it with another client",
		"Preinstalled for this environment: apt ffmpeg; pip pandas==2.2.3, httpx.",
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("prompt missing %q:\n%s", fragment, prompt)
		}
	}
}

func TestEnvironmentPromptLimitedWithoutHosts(t *testing.T) {
	input, err := environmentPromptInputFromConfig(json.RawMessage(`{"type":"cloud","networking":{"type":"limited"}}`))
	if err != nil {
		t.Fatal(err)
	}
	prompt := renderEnvironmentPrompt(input)
	if !strings.Contains(prompt, "No general hosts are allowed.") || strings.Contains(prompt, "also reachable") {
		t.Fatalf("unexpected limited network section:\n%s", prompt)
	}
}

func TestEnvironmentPackageSummaryCapsSpecs(t *testing.T) {
	specs := make([]string, maxPromptPackageSpecs+5)
	for index := range specs {
		specs[index] = "pkg" + strings.Repeat("x", index%3)
	}
	summary := environmentPackageSummary(&environmentPackages{NPM: specs, PIP: []string{"httpx"}})
	if !strings.HasSuffix(summary, "; and 6 more") || strings.Contains(summary, "pip") {
		t.Fatalf("summary = %q", summary)
	}
}

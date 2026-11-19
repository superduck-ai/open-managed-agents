package environments

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/clitools"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"strings"
	"testing"
)

func TestManagedRuntimePromptAndSkills(t *testing.T) {
	if _, err := managedAgentSessionConfig(db.Session{AgentSnapshot: json.RawMessage(`invalid`)}, managedAgentRuntimeResources{}, nil, ""); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	runtimeSkills := []skillsapi.RuntimeSkill{{Name: "PDF", Description: "Read PDF", Directory: "pdf", Version: "v1"}}
	raw, err := managedAgentSessionConfig(db.Session{AgentSnapshot: json.RawMessage(`{"system":"literal ${{ instructions }}"}`)}, managedAgentRuntimeResources{runtimeSkills: runtimeSkills}, runtimeSkills, "")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		System string `json:"system_prompt"`
		Append string `json:"append_system_prompt"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config.System != "literal ${{ instructions }}" || config.Append != mustEnvironmentPrompt(t, nil) || !strings.Contains(string(raw), "/root/.claude/skills/pdf/SKILL.md") {
		t.Fatalf("invalid shared startup config: %s", raw)
	}
}

func TestInstalledCLIProviderSnapshot(t *testing.T) {
	raw, err := managedAgentSessionConfig(db.Session{}, managedAgentRuntimeResources{
		cliTools:          []clitools.Binding{{Definition: clitools.Definition{Variable: "LOOKUP_TOKEN"}, Package: clitools.Package{ProviderVariable: "LOOKUP_PROVIDER"}}},
		environmentConfig: json.RawMessage(`{"type":"cloud","environment":{"LOOKUP_PROVIDER":"custom:example"}}`),
	}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Providers map[string]string `json:"cli_oauth_providers"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Providers["LOOKUP_TOKEN"] != "custom:example" {
		t.Fatalf("Catalog 客户端引用未保存到会话配置：%v", err)
	}
}

package environments

import (
	"encoding/json"
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

func TestFridayProviderSnapshot(t *testing.T) {
	raw, err := managedAgentSessionConfig(db.Session{}, managedAgentRuntimeResources{
		environmentConfig: json.RawMessage(`{"type":"cloud","environment":{"OMA_FRIDAY_OAUTH_PROVIDER":"custom:example"}}`),
	}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Provider string `json:"friday_oauth_provider"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Provider != "custom:example" {
		t.Fatalf("Friday 客户端引用未保存到会话配置：%v", err)
	}
}

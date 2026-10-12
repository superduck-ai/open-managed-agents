package environments

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"strings"
	"testing"
)

func TestManagedRuntimePromptAndSkills(t *testing.T) {
	if _, err := managedAgentSessionConfig(db.Session{AgentSnapshot: json.RawMessage(`invalid`)}, managedAgentRuntimeResources{}); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	raw, err := managedAgentSessionConfig(db.Session{AgentSnapshot: json.RawMessage(`{"system":"literal ${{ instructions }}"}`)}, managedAgentRuntimeResources{runtimeSkills: []skillsapi.RuntimeSkill{{Name: "PDF", Description: "Read PDF", Directory: "pdf", Version: "v1"}}})
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
	if config.System != "literal ${{ instructions }}" || config.Append != managedAgentEnvironmentPrompt || !strings.Contains(string(raw), "/root/.claude/skills/pdf/SKILL.md") {
		t.Fatalf("invalid shared startup config: %s", raw)
	}
}

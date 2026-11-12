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

func TestManagedRuntimeCatalogDoesNotAdvertisePhysicalUnion(t *testing.T) {
	snapshot := json.RawMessage(`{"multiagent":{"agents":[{"id":"agent_worker","version":1,"name":"worker","skills":[]},{"id":"agent_empty","version":2,"name":"empty","skills":[]}]}}`)
	values := []skillsapi.RuntimeSkill{{Name: "demo", Directory: "demo", InstallDirectory: "version-b", Version: "B", Bindings: []skillsapi.RuntimeSkillBinding{{RequestedVersion: "latest"}}}, {Name: "demo", Directory: "demo", InstallDirectory: "version-a", Version: "A", Bindings: []skillsapi.RuntimeSkillBinding{{Scope: "worker", AgentID: "agent_worker", AgentVersion: 1, RequestedVersion: "A"}}}}
	raw, err := managedAgentSessionConfig(db.Session{AgentSnapshot: snapshot}, managedAgentRuntimeResources{runtimeSkills: values}, values, "")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Version int                                     `json:"skill_catalog_version"`
		Root    []skillsapi.RuntimeCatalogEntry         `json:"runtime_skills"`
		Agents  map[string]skillsapi.RuntimeAgentSkills `json:"agent_skills"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Version != 1 || len(result.Root) != 1 || result.Root[0].Version != "B" || len(result.Agents["worker"].Skills) != 1 || result.Agents["worker"].Skills[0].Version != "A" || result.Agents["empty"].Skills == nil {
		t.Fatalf("scope lost in actual launch configuration: %s", raw)
	}
}

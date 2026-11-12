package codesessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"testing"
)

func TestDSHScopedSkillCatalogRejectsMissingAuthority(t *testing.T) {
	for _, mode := range []string{"missing", "foreign", "wrong-version", "null"} {
		catalog := skillsapi.RuntimeAgentSkills{AgentID: "agent_worker", AgentVersion: 3, Skills: []runtimeSkillConfig{}}
		catalogs := map[string]skillsapi.RuntimeAgentSkills{"worker": catalog}
		switch mode {
		case "missing":
			delete(catalogs, "worker")
		case "foreign":
			catalog.AgentID = "foreign"
			catalogs["worker"] = catalog
		case "wrong-version":
			catalog.AgentVersion = 4
			catalogs["worker"] = catalog
		case "null":
			catalog.Skills = nil
			catalogs["worker"] = catalog
		}
		source, _ := json.Marshal(map[string]any{"skill_catalog_version": 1, "agent_skills": catalogs, "subagents": RuntimeSubagentConfig{Agents: []RuntimeSubagentDefinition{{Name: "worker", AgentSnapshot: json.RawMessage(`{"id":"agent_worker","version":3,"tools":[],"mcp_servers":[]}`)}}}})
		if _, err := dshChildContexts(db.CodeSession{Metadata: json.RawMessage(`{"config":{}}`)}, dshContextSnapshot{Config: source}, MCPRuntimeIdentity{}, config.TunnelConfig{}); err == nil {
			t.Fatal("unproven catalogue accepted", mode)
		}
	}
}

func TestDSHScopedSkillCatalogPreservesEmptyAndPinnedChild(t *testing.T) {
	entry := runtimeSkillConfig{Name: "demo", Version: "1", Path: "/root/.claude/skills/version-one/SKILL.md", SkillID: "skill_a", VersionUUID: "00000000-0000-4000-8000-000000000001", Source: "custom", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RequestedVersions: []string{"1"}}
	source, _ := json.Marshal(map[string]any{"skill_catalog_version": 1, "runtime_skills": []runtimeSkillConfig{{Name: "demo", Version: "2", Path: "/root/.claude/skills/version-two/SKILL.md"}}, "agent_skills": map[string]skillsapi.RuntimeAgentSkills{"worker": {AgentID: "agent_worker", AgentVersion: 3, Skills: []runtimeSkillConfig{entry}}, "empty": {AgentID: "agent_empty", AgentVersion: 2, Skills: []runtimeSkillConfig{}}}, "subagents": RuntimeSubagentConfig{Agents: []RuntimeSubagentDefinition{{Name: "worker", AgentSnapshot: json.RawMessage(`{"id":"agent_worker","version":3,"tools":[],"mcp_servers":[]}`)}, {Name: "empty", AgentSnapshot: json.RawMessage(`{"id":"agent_empty","version":2,"tools":[],"mcp_servers":[]}`)}}}})
	record := db.CodeSession{Metadata: json.RawMessage(`{"config":{}}`)}
	for _, owner := range []string{"initial", "replacement"} {
		children, err := dshChildContexts(record, dshContextSnapshot{Config: source}, MCPRuntimeIdentity{SessionIngressToken: owner}, config.TunnelConfig{})
		if err != nil {
			t.Fatal(err)
		}
		if len(children["worker"].RuntimeSkills) != 1 || children["worker"].RuntimeSkills[0].Version != "1" || children["empty"].RuntimeSkills == nil || len(children["empty"].RuntimeSkills) != 0 {
			t.Fatal("root scope leaked", children)
		}
		wire, err := json.Marshal(children)
		if err != nil {
			t.Fatal(err)
		}
		var restored map[string]dshChildContext
		if json.Unmarshal(wire, &restored) != nil || restored["empty"].RuntimeSkills == nil {
			t.Fatal("empty child catalogue lost on wire")
		}
	}
}

package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/agents"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func applySessionConfigurationOverrides(agent db.Agent, tools, servers, skills json.RawMessage) (db.Agent, error) {
	if len(skills) > 0 {
		normalized, err := agents.NormalizeSessionSkillsOverride(skills)
		if err != nil {
			return db.Agent{}, err
		}
		agent.Skills = normalized
	}
	effective, err := applySessionToolConfigurationOverrides(agent, tools, servers)
	if err != nil {
		return db.Agent{}, err
	}
	if len(skills) > 0 && len(tools) == 0 && len(servers) == 0 {
		if _, err = agents.NormalizeSessionToolsOverride(effective.Tools, effective.MCPServers, effective.Skills); err != nil {
			return db.Agent{}, err
		}
	}
	return effective, nil
}

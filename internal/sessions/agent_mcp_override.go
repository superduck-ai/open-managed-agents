package sessions

import (
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/agents"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func applySessionToolConfigurationOverrides(agent db.Agent, tools, servers json.RawMessage) (db.Agent, error) {
	if len(servers) > 0 {
		normalized, err := agents.NormalizeSessionMCPServersOverride(servers)
		if err != nil {
			return db.Agent{}, err
		}
		agent.MCPServers = normalized
	}
	if len(tools) > 0 {
		return applySessionToolsOverride(agent, tools)
	}
	if len(servers) > 0 {
		if _, err := agents.NormalizeSessionToolsOverride(agent.Tools, agent.MCPServers, agent.Skills); err != nil {
			return db.Agent{}, err
		}
	}
	return agent, nil
}

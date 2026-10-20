package sessions

import (
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/agents"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func applySessionToolsOverride(agent db.Agent, raw json.RawMessage) (db.Agent, error) {
	if len(raw) == 0 {
		return agent, nil
	}
	tools, err := agents.NormalizeSessionToolsOverride(raw, agent.MCPServers, agent.Skills)
	if err != nil {
		return db.Agent{}, err
	}
	agent.Tools = tools
	return agent, nil
}

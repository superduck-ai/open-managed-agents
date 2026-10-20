package sessions

import (
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type inlineThreadAgent struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	System      string          `json:"system"`
	Model       json.RawMessage `json:"model"`
	Tools       json.RawMessage `json:"tools"`
	MCPServers  json.RawMessage `json:"mcp_servers"`
	Skills      json.RawMessage `json:"skills"`
	Metadata    json.RawMessage `json:"metadata"`
}

func inlineSessionThreadAgentSnapshot(agent map[string]any, parent json.RawMessage) (json.RawMessage, error) {
	var snapshot inlineThreadAgent
	if err := json.Unmarshal(parent, &snapshot); err != nil {
		return nil, err
	}
	snapshot.Type = "inline"
	snapshot.Name = sessionPayloadString(agent, "name")
	snapshot.Description = nil
	snapshot.System, _ = agent["system"].(string)
	return httpapi.MarshalRaw(snapshot)
}

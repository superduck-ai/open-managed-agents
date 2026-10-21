package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type sessionAgentToolUpdate struct {
	Tools      json.RawMessage
	MCPServers json.RawMessage
}

type sessionAgentToolSnapshot struct {
	Tools      json.RawMessage `json:"tools"`
	MCPServers json.RawMessage `json:"mcp_servers"`
	Skills     json.RawMessage `json:"skills"`
}

func parseSessionAgentToolUpdate(raw json.RawMessage) (sessionAgentToolUpdate, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return sessionAgentToolUpdate{}, errSessionAgentUpdateObjectRequired
	}
	for key := range fields {
		if key != "tools" && key != "mcp_servers" {
			return sessionAgentToolUpdate{}, errSessionAgentUpdateUnsupportedFields
		}
	}
	return sessionAgentToolUpdate{Tools: fields["tools"], MCPServers: fields["mcp_servers"]}, nil
}

func patchSessionAgent(current json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
	patch, err := parseSessionAgentToolUpdate(raw)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(current, &fields); err != nil || fields == nil {
		return nil, errStoredSessionAgentInvalid
	}
	var snapshot sessionAgentToolSnapshot
	if err = json.Unmarshal(current, &snapshot); err != nil {
		return nil, errStoredSessionAgentInvalid
	}
	effective, err := applySessionToolConfigurationOverrides(db.Agent{Tools: rawOrDefault(snapshot.Tools, "[]"), MCPServers: rawOrDefault(snapshot.MCPServers, "[]"), Skills: snapshot.Skills}, patch.Tools, patch.MCPServers)
	if err != nil {
		return nil, err
	}
	if len(patch.Tools) > 0 {
		fields["tools"] = effective.Tools
	}
	if len(patch.MCPServers) > 0 {
		fields["mcp_servers"] = effective.MCPServers
	}
	return json.Marshal(fields)
}

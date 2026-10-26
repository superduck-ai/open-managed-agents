package codesessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (h *Handler) currentSessionToolContext(ctx context.Context, record db.CodeSession) (db.CodeSession, error) {
	if record.SessionExternalID == "" {
		return record, nil
	}
	session, found, err := h.db.GetSession(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return db.CodeSession{}, err
	}
	if !found {
		return db.CodeSession{}, db.ErrNotFound
	}
	return codeSessionWithToolSnapshot(record, session.AgentSnapshot)
}

func codeSessionWithToolSnapshot(record db.CodeSession, snapshot json.RawMessage) (db.CodeSession, error) {
	var agent struct {
		Tools      json.RawMessage `json:"tools"`
		MCPServers json.RawMessage `json:"mcp_servers"`
	}
	if _, err := decodeRawJSONObject(snapshot); err != nil {
		return db.CodeSession{}, err
	}
	if err := json.Unmarshal(snapshot, &agent); err != nil {
		return db.CodeSession{}, err
	}
	metadata := map[string]json.RawMessage{}
	if len(record.Metadata) > 0 {
		var err error
		metadata, err = decodeRawJSONObject(record.Metadata)
		if err != nil {
			return db.CodeSession{}, err
		}
	}
	config := map[string]json.RawMessage{}
	if len(metadata["config"]) > 0 {
		var err error
		config, err = decodeRawJSONObject(metadata["config"])
		if err != nil {
			return db.CodeSession{}, err
		}
	}
	if len(agent.Tools) == 0 {
		agent.Tools = json.RawMessage(`[]`)
	}
	if len(agent.MCPServers) == 0 {
		agent.MCPServers = json.RawMessage(`[]`)
	}
	config["tools"] = agent.Tools
	config["mcp_servers"] = agent.MCPServers
	delete(config, "mcp_config")
	encoded, err := json.Marshal(config)
	if err != nil {
		return db.CodeSession{}, err
	}
	metadata["config"] = encoded
	record.Metadata, err = json.Marshal(metadata)
	return record, err
}

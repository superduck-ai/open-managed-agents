package codesessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
)

func (e dshExecution) validatePublicThreads(ctx context.Context, scope db.DSHScope, batch []db.DSHPublicEvent) error {
	for i := range batch {
		if batch[i].Thread == nil {
			continue
		}
		session, found, err := e.handler.db.GetSession(ctx, scope.WorkspaceUUID, scope.SessionID)
		if err != nil {
			return err
		}
		if !found || session.OrganizationUUID != scope.OrganizationUUID {
			return db.ErrNotFound
		}
		snapshot, err := e.handler.db.GetDSHContextSnapshot(ctx, scope)
		if err != nil {
			return err
		}
		config, err := registeredDSHSubagents(snapshot)
		if err != nil {
			return err
		}
		raw, err := trustedDSHChildSnapshot(batch[i].Thread.AgentSnapshot, session.AgentSnapshot, config)
		if err != nil {
			return err
		}
		batch[i].Thread.AgentSnapshot = raw
	}
	return nil
}

func trustedDSHChildSnapshot(raw, parent json.RawMessage, config *RuntimeSubagentConfig) (json.RawMessage, error) {
	if config == nil {
		return nil, db.ErrInvalidState
	}
	var child struct {
		Type   string `json:"type"`
		Name   string `json:"name"`
		System string `json:"system"`
	}
	if err := json.Unmarshal(raw, &child); err != nil {
		return nil, db.ErrInvalidState
	}
	if child.Type == "inline" {
		if !config.InlineAgents || child.Name == "" {
			return nil, db.ErrInvalidState
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(parent, &fields); err != nil {
			return nil, err
		}
		setRawJSONField(fields, "type", "inline")
		setRawJSONField(fields, "name", child.Name)
		setRawJSONField(fields, "system", child.System)
		delete(fields, "description")
		delete(fields, "id")
		delete(fields, "version")
		delete(fields, "multiagent")
		return json.Marshal(fields)
	}
	for _, definition := range config.Agents {
		if definition.Name != child.Name {
			continue
		}
		equal, err := jsoncanonical.Equal(raw, definition.AgentSnapshot)
		if err != nil {
			return nil, err
		}
		if equal {
			return definition.AgentSnapshot, nil
		}
	}
	return nil, db.ErrInvalidState
}

func registeredDSHSubagents(raw []byte) (*RuntimeSubagentConfig, error) {
	if len(raw) == 0 {
		return nil, db.ErrInvalidState
	}
	var snapshot dshContextSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	config, err := parseManagedAgentRuntimeConfig(snapshot.Config)
	if err != nil {
		return nil, err
	}
	if config.Subagents == nil {
		return nil, db.ErrInvalidState
	}
	return config.Subagents, nil
}

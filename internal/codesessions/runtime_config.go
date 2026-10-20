package codesessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type runtimeSkillConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Path        string `json:"path"`
}

type managedAgentRuntimeConfigSchema struct {
	Model               string                 `json:"model"`
	SystemPrompt        string                 `json:"system_prompt"`
	CustomSystemPrompt  string                 `json:"custom_system_prompt"`
	AppendSystemPrompt  string                 `json:"append_system_prompt"`
	MemoryCatalogPrompt string                 `json:"memory_catalog_prompt"`
	RuntimeSkills       []runtimeSkillConfig   `json:"runtime_skills"`
	Subagents           *RuntimeSubagentConfig `json:"subagents"`
	RuntimeTools        []string               `json:"runtime_tools"`
	RuntimeToolPolicies map[string]string      `json:"runtime_tool_permissions"`
	fields              map[string]json.RawMessage
}

func parseManagedAgentRuntimeConfig(raw json.RawMessage) (managedAgentRuntimeConfigSchema, error) {
	var config managedAgentRuntimeConfigSchema
	if len(raw) == 0 {
		config.fields = make(map[string]json.RawMessage)
		return config, nil
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return managedAgentRuntimeConfigSchema{}, err
	}
	if err := json.Unmarshal(raw, &config.fields); err != nil {
		return managedAgentRuntimeConfigSchema{}, err
	}
	if config.fields == nil {
		config.fields = make(map[string]json.RawMessage)
	}
	return config, nil
}

func (c managedAgentRuntimeConfigSchema) encode() (json.RawMessage, error) {
	if c.Subagents != nil {
		encoded, err := json.Marshal(c.Subagents)
		if err != nil {
			return nil, err
		}
		c.fields["subagents"] = encoded
	}
	if c.RuntimeTools != nil {
		encoded, err := json.Marshal(c.RuntimeTools)
		if err != nil {
			return nil, err
		}
		c.fields["runtime_tools"] = encoded
	}
	if c.RuntimeToolPolicies != nil {
		encoded, err := json.Marshal(c.RuntimeToolPolicies)
		if err != nil {
			return nil, err
		}
		c.fields["runtime_tool_permissions"] = encoded
	}
	return json.Marshal(c.fields)
}

func runtimeRootToolsets(agentSnapshot json.RawMessage) ([]toolPermissionToolset, error) {
	var snapshot struct {
		Tools []toolPermissionToolset `json:"tools"`
	}
	if len(agentSnapshot) > 0 {
		if err := json.Unmarshal(agentSnapshot, &snapshot); err != nil {
			return nil, err
		}
	}
	return snapshot.Tools, nil
}

func runtimeToolPolicies(tools []toolPermissionToolset) map[string]string {
	policies := make(map[string]string, len(sandboxToolNames))
	for _, name := range sandboxToolNames {
		remoteName := managedAgentToolName(name)
		policies[remoteName] = string(resolveAgentToolPermission(tools, remoteName))
	}
	return policies
}

func (s *Service) currentRuntimeToolPolicies(ctx context.Context, record db.CodeSession) (map[string]string, error) {
	if record.SessionExternalID == "" {
		return nil, nil
	}
	session, found, err := s.db.GetSession(ctx, record.WorkspaceUUID, record.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	tools, err := runtimeRootToolsets(session.AgentSnapshot)
	if err != nil {
		return nil, err
	}
	return runtimeToolPolicies(tools), nil
}

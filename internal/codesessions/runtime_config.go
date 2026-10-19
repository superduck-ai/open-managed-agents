package codesessions

import "encoding/json"

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
	return json.Marshal(c.fields)
}

func runtimeRootTools(agentSnapshot json.RawMessage) ([]string, error) {
	var snapshot struct {
		Tools []toolPermissionToolset `json:"tools"`
	}
	if len(agentSnapshot) > 0 {
		if err := json.Unmarshal(agentSnapshot, &snapshot); err != nil {
			return nil, err
		}
	}
	return enabledSandboxTools(snapshot.Tools), nil
}

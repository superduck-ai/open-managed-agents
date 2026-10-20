package agents

import (
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type sessionBuiltinToolset struct {
	Type          string `json:"type"`
	DefaultConfig struct {
		Enabled bool `json:"enabled"`
	} `json:"default_config"`
	Configs []struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	} `json:"configs"`
}

func NormalizeSessionToolsOverride(raw, mcpServers, skills json.RawMessage) (json.RawMessage, error) {
	tools, err := normalizeTools(raw, mcpServers)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 || httpapi.IsJSONNull(skills) {
		return tools, nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(skills, &entries) != nil {
		return nil, errSessionSkillReadRequired
	}
	if len(entries) > 0 && !sessionToolsEnableRead(tools) {
		return nil, errSessionSkillReadRequired
	}
	return tools, nil
}

func sessionToolsEnableRead(raw json.RawMessage) bool {
	var toolsets []sessionBuiltinToolset
	if json.Unmarshal(raw, &toolsets) != nil {
		return false
	}
	for _, toolset := range toolsets {
		if toolset.Type != "agent_toolset_20260401" {
			continue
		}
		enabled := toolset.DefaultConfig.Enabled
		for _, config := range toolset.Configs {
			if config.Name == "read" {
				enabled = config.Enabled
			}
		}
		return enabled
	}
	return false
}

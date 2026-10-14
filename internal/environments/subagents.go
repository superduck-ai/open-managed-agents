package environments

import (
	"cmp"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
)

const managedAgentDelegationPrompt = `## Subagent coordination

- SendMessage resumes a child in the background. To wait for its result, use TaskOutput with its runtime task ID, block=true and timeout=600000 when the tool is available.
- Child completion messages arrive automatically. If a child is waiting for user approval, return control to the user. Do not repeatedly check its status.
- ReadNotifications reads external notifications, not child completion messages. Do not poll it to wait for subagents.`

type subagentLaunchSnapshot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	System      string `json:"system"`
	Model       struct {
		ID string `json:"id"`
	} `json:"model"`
	Skills []struct {
		Type    string `json:"type"`
		SkillID string `json:"skill_id"`
		Version string `json:"version"`
	} `json:"skills"`
}

type claudeAgentDefinition struct {
	Description string                       `json:"description"`
	Prompt      string                       `json:"prompt"`
	Model       string                       `json:"model"`
	Skills      []string                     `json:"skills,omitempty"`
	MCPServers  []map[string]json.RawMessage `json:"mcpServers,omitempty"`
}

func buildManagedAgentSubagents(source, document json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Multiagent *struct {
			Agents []json.RawMessage `json:"agents"`
		} `json:"multiagent"`
		SkillDirectories map[string]string          `json:"skill_directories"`
		ClaudeCodeArgs   map[string]json.RawMessage `json:"claude_code_args"`
	}
	if err := jsonv2.Unmarshal(source, &input); err != nil {
		return nil, err
	}
	if input.Multiagent == nil {
		return source, nil
	}
	var mcp struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if len(document) > 0 {
		if err := jsonv2.Unmarshal(document, &mcp); err != nil {
			return nil, err
		}
	}
	var servers []map[string]json.RawMessage
	for name, server := range mcp.Servers {
		servers = append(servers, map[string]json.RawMessage{strings.ReplaceAll(name, ".", "_"): server})
	}
	agents := make(map[string]claudeAgentDefinition, len(input.Multiagent.Agents))
	for _, raw := range input.Multiagent.Agents {
		var snapshot subagentLaunchSnapshot
		if err := jsonv2.Unmarshal(raw, &snapshot); err != nil {
			return nil, err
		}
		if snapshot.ID == "" || snapshot.Model.ID == "" {
			return nil, fmt.Errorf("subagent snapshot must include id and model")
		}
		if snapshot.Name == "" || strings.HasPrefix(snapshot.Name, "-") {
			return nil, fmt.Errorf("subagent name must be non-empty and must not start with '-'")
		}
		if _, exists := agents[snapshot.Name]; exists {
			return nil, fmt.Errorf("duplicate subagent name: %s", snapshot.Name)
		}
		definition := claudeAgentDefinition{Description: snapshot.Description, Prompt: snapshot.System + "\n\n" + managedAgentEnvironmentPrompt, Model: snapshot.Model.ID, MCPServers: servers}
		if definition.Description == "" {
			definition.Description = snapshot.Name
		} else {
			definition.Description = snapshot.Name + ": " + definition.Description
		}
		for _, skill := range snapshot.Skills {
			version := cmp.Or(strings.TrimSpace(skill.Version), "latest")
			directory := input.SkillDirectories[skill.Type+":"+skill.SkillID+":"+version]
			if directory == "" {
				return nil, fmt.Errorf("subagent skill directory missing: %s", skill.SkillID)
			}
			definition.Skills = append(definition.Skills, directory)
		}
		agents[snapshot.Name] = definition
	}
	encoded, err := jsonv2.Marshal(agents)
	if err != nil {
		return nil, err
	}
	if input.ClaudeCodeArgs == nil {
		input.ClaudeCodeArgs = make(map[string]json.RawMessage)
	}
	input.ClaudeCodeArgs["agents"], err = jsonv2.Marshal(string(encoded))
	if err != nil {
		return nil, err
	}
	return mergeManagedAgentMCPFields(source, struct {
		Args map[string]json.RawMessage `json:"claude_code_args"`
	}{Args: input.ClaudeCodeArgs})
}

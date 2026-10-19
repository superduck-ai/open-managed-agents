package codesessions

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type RuntimeSubagentConfig struct {
	InlineAgents bool                        `json:"inlineAgents"`
	Agents       []RuntimeSubagentDefinition `json:"agents"`
}

type RuntimeSubagentDefinition struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"systemPrompt"`
	Model        string   `json:"model,omitempty"`
	Tools        []string `json:"tools"`
}

type runtimeAgentReference struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version int    `json:"version"`
}

type runtimeAgentSnapshot struct {
	ID          string                  `json:"id"`
	Version     int                     `json:"version"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	System      string                  `json:"system"`
	Model       json.RawMessage         `json:"model"`
	Tools       []toolPermissionToolset `json:"tools"`
	Multiagent  *struct {
		Type   string                  `json:"type"`
		Agents []runtimeAgentReference `json:"agents"`
	} `json:"multiagent"`
}

func resolveRuntimeSubagents(ctx context.Context, store *db.DB, session db.Session) (*RuntimeSubagentConfig, error) {
	if len(session.AgentSnapshot) == 0 {
		return nil, nil
	}
	var snapshot runtimeAgentSnapshot
	if err := json.Unmarshal(session.AgentSnapshot, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Multiagent == nil {
		return nil, nil
	}
	roster := snapshot.Multiagent
	if roster.Type != "coordinator" || len(roster.Agents) < 1 || len(roster.Agents) > 20 {
		return nil, errRuntimeSubagentConfig
	}
	config := &RuntimeSubagentConfig{Agents: make([]RuntimeSubagentDefinition, 0, len(roster.Agents))}
	seen := make(map[string]bool, len(roster.Agents))
	for _, ref := range roster.Agents {
		child, err := resolveRuntimeAgent(ctx, store, session.WorkspaceUUID, snapshot, ref)
		if err != nil {
			return nil, err
		}
		definition, err := runtimeSubagentDefinition(child)
		if err != nil {
			return nil, err
		}
		if seen[definition.Name] {
			return nil, fmt.Errorf("%w: duplicate subagent name %q", errRuntimeSubagentConfig, definition.Name)
		}
		seen[definition.Name] = true
		config.Agents = append(config.Agents, definition)
	}
	return config, nil
}

func resolveRuntimeAgent(ctx context.Context, store *db.DB, workspaceUUID string, parent runtimeAgentSnapshot, ref runtimeAgentReference) (runtimeAgentSnapshot, error) {
	if ref.Type != "agent" || ref.ID == "" || ref.Version < 1 {
		return runtimeAgentSnapshot{}, errRuntimeSubagentConfig
	}
	if ref.ID == parent.ID && ref.Version == parent.Version {
		return parent, nil
	}
	agent, err := store.GetAgentVersion(ctx, workspaceUUID, ref.ID, ref.Version)
	if err != nil {
		return runtimeAgentSnapshot{}, fmt.Errorf("resolve runtime subagent %s version %d: %w", ref.ID, ref.Version, err)
	}
	if agent.WorkspaceUUID != workspaceUUID || agent.ExternalID != ref.ID || agent.CurrentVersion != ref.Version {
		return runtimeAgentSnapshot{}, errRuntimeSubagentConfig
	}
	raw, err := agentsnapshot.FromAgent(agent)
	if err != nil {
		return runtimeAgentSnapshot{}, err
	}
	var snapshot runtimeAgentSnapshot
	err = json.Unmarshal(raw, &snapshot)
	return snapshot, err
}

func runtimeSubagentDefinition(snapshot runtimeAgentSnapshot) (RuntimeSubagentDefinition, error) {
	definition := RuntimeSubagentDefinition{
		Name: snapshot.Name, Description: snapshot.Description, SystemPrompt: snapshot.System,
		Tools: make([]string, 0),
	}
	if definition.Name == "" {
		return definition, errRuntimeSubagentConfig
	}
	if len(snapshot.Model) > 0 && string(snapshot.Model) != "null" {
		if err := json.Unmarshal(snapshot.Model, &definition.Model); err != nil {
			var model struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(snapshot.Model, &model); err != nil {
				return definition, err
			}
			definition.Model = model.ID
		}
	}
	for _, toolset := range snapshot.Tools {
		if toolset.Type == "agent_toolset_20260401" {
			definition.Tools = enabledSandboxTools(snapshot.Tools)
			break
		}
	}
	return definition, nil
}

func enabledSandboxTools(tools []toolPermissionToolset) []string {
	enabled := make([]string, 0, 6)
	for _, name := range []string{"Bash", "Read", "Write", "Edit", "Grep", "Glob"} {
		if resolveAgentToolPermission(tools, managedAgentToolName(name)) != resolvedToolPermissionDeny {
			enabled = append(enabled, name)
		}
	}
	return enabled
}

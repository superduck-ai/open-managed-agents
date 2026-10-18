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
	AgentSnapshot json.RawMessage `json:"agentSnapshot,omitempty"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	SystemPrompt  string          `json:"systemPrompt"`
	Model         string          `json:"model,omitempty"`
	Tools         []string        `json:"tools"`
}

type runtimeAgentReference struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version int    `json:"version"`
}

type runtimeAgentSnapshot struct {
	publicSnapshot json.RawMessage
	ID             string                  `json:"id"`
	Version        int                     `json:"version"`
	Name           string                  `json:"name"`
	Description    string                  `json:"description"`
	System         string                  `json:"system"`
	Model          json.RawMessage         `json:"model"`
	Tools          []toolPermissionToolset `json:"tools"`
	Multiagent     *struct {
		Type      string                  `json:"type"`
		Agents    []runtimeAgentReference `json:"agents"`
		Subagents *runtimeDelegation      `json:"subagents"`
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
	snapshot.publicSnapshot = session.AgentSnapshot
	if snapshot.Multiagent == nil {
		return nil, nil
	}
	roster := snapshot.Multiagent
	inline := false
	switch roster.Type {
	case "coordinator":
		if len(roster.Agents) < 1 {
			return nil, errRuntimeSubagentConfig
		}
	case "multiagent_20261001":
		inline = true
		if roster.Subagents != nil {
			if roster.Subagents.Type == "disabled" {
				return nil, nil
			}
			if roster.Subagents.Type != "enabled" {
				return nil, errRuntimeSubagentConfig
			}
			inline = roster.Subagents.InlineAgents == nil || roster.Subagents.InlineAgents.Type == "enabled"
			roster.Agents = roster.Subagents.PredefinedAgents
		}
	default:
		return nil, errRuntimeSubagentConfig
	}
	if len(roster.Agents) > 20 || (!inline && len(roster.Agents) == 0) {
		return nil, errRuntimeSubagentConfig
	}
	config := &RuntimeSubagentConfig{InlineAgents: inline, Agents: make([]RuntimeSubagentDefinition, 0, len(roster.Agents))}
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
	snapshot.publicSnapshot = raw
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
	definition.Tools = runtimeBuiltinTools(snapshot.Tools)
	if len(snapshot.publicSnapshot) > 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(snapshot.publicSnapshot, &fields); err != nil {
			return definition, err
		}
		delete(fields, "multiagent")
		fields["type"] = json.RawMessage(`"agent"`)
		var err error
		definition.AgentSnapshot, err = json.Marshal(fields)
		if err != nil {
			return definition, err
		}
	}
	return definition, nil
}

type runtimeDelegation struct {
	Type         string `json:"type"`
	InlineAgents *struct {
		Type string `json:"type"`
	} `json:"inline_agents"`
	PredefinedAgents []runtimeAgentReference `json:"predefined_agents"`
}

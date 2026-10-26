package agentsnapshot

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type rosterReference struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type subagentRoster struct {
	Type      string            `json:"type"`
	Agents    []json.RawMessage `json:"agents,omitempty"`
	Subagents *delegationRoster `json:"subagents,omitempty"`
	Workflows *delegationRoster `json:"workflows,omitempty"`
	Advisor   json.RawMessage   `json:"advisor,omitempty"`
}

func Resolve(ctx context.Context, database *db.DB, agent db.Agent) (json.RawMessage, error) {
	if len(agent.Multiagent) == 0 {
		return FromAgent(agent)
	}
	var roster *subagentRoster
	if err := jsonv2.Unmarshal(agent.Multiagent, &roster); err != nil {
		return nil, err
	}
	if roster == nil {
		return FromAgent(agent)
	}
	if roster.Type == "multiagent_20261001" {
		for _, setting := range []*delegationRoster{roster.Subagents, roster.Workflows} {
			if setting == nil || setting.Type != "enabled" {
				continue
			}
			entries, err := expandRoster(ctx, database, agent, setting.PredefinedAgents)
			if err != nil {
				return nil, err
			}
			if err := validateRosterGeography(agent.Model, entries); err != nil {
				return nil, err
			}
			setting.PredefinedAgents = entries
		}
	} else {
		entries, err := expandRoster(ctx, database, agent, roster.Agents)
		if err != nil {
			return nil, err
		}
		roster.Agents = entries
	}

	var err error
	agent.Multiagent, err = jsonv2.Marshal(roster)
	if err != nil {
		return nil, err
	}
	return FromAgent(agent)
}

type delegationRoster struct {
	Type             string            `json:"type"`
	InlineAgents     json.RawMessage   `json:"inline_agents,omitempty"`
	PredefinedAgents []json.RawMessage `json:"predefined_agents,omitzero"`
}

func expandRoster(ctx context.Context, database *db.DB, agent db.Agent, entries []json.RawMessage) ([]json.RawMessage, error) {
	seenNames := make(map[string]struct{}, len(entries))
	for i, raw := range entries {
		var ref rosterReference
		if err := jsonv2.Unmarshal(raw, &ref); err != nil {
			return nil, err
		}
		if ref.ID == "" || ref.Version < 1 {
			return nil, fmt.Errorf("subagent reference must include id and pinned version")
		}
		child := agent
		if ref.ID != agent.ExternalID || ref.Version != agent.CurrentVersion {
			var err error
			child, err = database.GetAgentVersion(ctx, agent.WorkspaceUUID, ref.ID, ref.Version)
			if err != nil {
				return nil, fmt.Errorf("resolve subagent %s@%d: %w", ref.ID, ref.Version, err)
			}
			if ref.ID != agent.ExternalID && len(child.Multiagent) > 0 && string(child.Multiagent) != "null" {
				return nil, fmt.Errorf("subagent %s must not configure multiagent", ref.ID)
			}
		}
		if child.Name == "" || strings.HasPrefix(child.Name, "-") {
			return nil, fmt.Errorf("subagent name must be non-empty and must not start with '-'")
		}
		if _, exists := seenNames[child.Name]; exists {
			return nil, fmt.Errorf("duplicate subagent name: %s", child.Name)
		}
		seenNames[child.Name] = struct{}{}
		child.Multiagent = nil
		expanded, err := FromAgent(child)
		if err != nil {
			return nil, err
		}
		entries[i] = expanded
	}
	return entries, nil
}

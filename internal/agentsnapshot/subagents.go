package agentsnapshot

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type rosterReference struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

func ForSubagent(snapshot json.RawMessage, agentID string) (json.RawMessage, error) {
	var root struct {
		Multiagent *struct {
			Agents []json.RawMessage `json:"agents"`
		} `json:"multiagent"`
	}
	if err := jsonv2.Unmarshal(snapshot, &root); err != nil {
		return nil, err
	}
	if root.Multiagent != nil {
		for _, child := range root.Multiagent.Agents {
			var identity rosterReference
			if err := jsonv2.Unmarshal(child, &identity); err != nil {
				return nil, err
			}
			if identity.ID == agentID {
				return child, nil
			}
		}
	}
	return nil, fmt.Errorf("configured subagent not found: %s", agentID)
}

func Resolve(ctx context.Context, database *db.DB, agent db.Agent) (json.RawMessage, error) {
	snapshot, err := FromAgent(agent)
	if err != nil {
		return nil, err
	}
	var roster struct {
		Type   string            `json:"type"`
		Agents []rosterReference `json:"agents"`
	}
	if len(agent.Multiagent) == 0 || string(agent.Multiagent) == "null" {
		return snapshot, nil
	}
	if err := jsonv2.Unmarshal(agent.Multiagent, &roster); err != nil {
		return nil, err
	}
	children := make([]json.RawMessage, 0, len(roster.Agents))
	for _, ref := range roster.Agents {
		child := agent
		if ref.ID != agent.ExternalID || ref.Version != agent.CurrentVersion {
			child, err = database.GetAgentVersion(ctx, agent.WorkspaceUUID, ref.ID, ref.Version)
			if err != nil {
				return nil, fmt.Errorf("resolve subagent %s@%d: %w", ref.ID, ref.Version, err)
			}
			if len(child.Multiagent) > 0 && string(child.Multiagent) != "null" {
				return nil, fmt.Errorf("subagent %s must not configure multiagent", ref.ID)
			}
		}
		child.Multiagent = nil
		raw, err := FromAgent(child)
		if err != nil {
			return nil, err
		}
		children = append(children, raw)
	}
	var fields map[string]json.RawMessage
	if err := jsonv2.Unmarshal(snapshot, &fields); err != nil {
		return nil, err
	}
	fields["multiagent"], err = jsonv2.Marshal(struct {
		Type   string            `json:"type"`
		Agents []json.RawMessage `json:"agents"`
	}{Type: roster.Type, Agents: children})
	if err != nil {
		return nil, err
	}
	return jsonv2.Marshal(fields)
}

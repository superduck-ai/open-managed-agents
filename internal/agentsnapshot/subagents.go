package agentsnapshot

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type rosterReference struct {
	ID      string          `json:"id"`
	Version int             `json:"version"`
	Model   json.RawMessage `json:"model"`
}

func Resolve(ctx context.Context, database *db.DB, agent db.Agent) (json.RawMessage, error) {
	snapshot, err := FromAgent(agent)
	if err != nil {
		return nil, err
	}
	return resolveSnapshot(ctx, database, agent.WorkspaceUUID, snapshot)
}

type agentRosterSnapshot struct {
	ID         string          `json:"id"`
	Version    int             `json:"version"`
	Multiagent *subagentRoster `json:"multiagent"`
}

type subagentRoster struct {
	Type   string            `json:"type"`
	Agents []json.RawMessage `json:"agents"`
}

func resolveSnapshot(ctx context.Context, database *db.DB, workspaceUUID string, snapshot json.RawMessage) (json.RawMessage, error) {
	var root agentRosterSnapshot
	if err := jsonv2.Unmarshal(snapshot, &root); err != nil {
		return nil, err
	}
	if root.Multiagent == nil {
		return snapshot, nil
	}
	var fields map[string]json.RawMessage
	for i, raw := range root.Multiagent.Agents {
		var ref rosterReference
		if err := jsonv2.Unmarshal(raw, &ref); err != nil {
			return nil, err
		}
		if len(ref.Model) > 0 {
			continue
		}
		if ref.ID == "" || ref.Version < 1 {
			return nil, fmt.Errorf("subagent reference must include id and pinned version")
		}
		if fields == nil {
			if err := jsonv2.Unmarshal(snapshot, &fields); err != nil {
				return nil, err
			}
		}
		if ref.ID == root.ID && ref.Version == root.Version {
			fields["multiagent"] = json.RawMessage(`null`)
			raw, err := jsonv2.Marshal(fields)
			if err != nil {
				return nil, err
			}
			root.Multiagent.Agents[i] = raw
		} else {
			child, err := database.GetAgentVersion(ctx, workspaceUUID, ref.ID, ref.Version)
			if err != nil {
				return nil, fmt.Errorf("resolve subagent %s@%d: %w", ref.ID, ref.Version, err)
			}
			if ref.ID != root.ID && len(child.Multiagent) > 0 && string(child.Multiagent) != "null" {
				return nil, fmt.Errorf("subagent %s must not configure multiagent", ref.ID)
			}
			child.Multiagent = nil
			raw, err := FromAgent(child)
			if err != nil {
				return nil, err
			}
			root.Multiagent.Agents[i] = raw
		}
	}
	if fields == nil {
		return snapshot, nil
	}
	var err error
	fields["multiagent"], err = jsonv2.Marshal(root.Multiagent)
	if err != nil {
		return nil, err
	}
	return jsonv2.Marshal(fields)
}

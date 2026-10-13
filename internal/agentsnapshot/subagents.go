package agentsnapshot

import (
	"cmp"
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

func subagentSnapshots(snapshot json.RawMessage) ([]json.RawMessage, error) {
	var root struct {
		Multiagent *struct {
			Agents []json.RawMessage `json:"agents"`
		} `json:"multiagent"`
	}
	if err := jsonv2.Unmarshal(snapshot, &root); err != nil {
		return nil, err
	}
	if root.Multiagent == nil {
		return nil, nil
	}
	return root.Multiagent.Agents, nil
}

func SubagentNames(children []json.RawMessage) ([]string, error) {
	names := make([]string, len(children))
	used := map[string]bool{"Explore": true, "Plan": true, "general-purpose": true, "claude": true, "statusline-setup": true, "claude-code-guide": true, "fork": true}
	for i, raw := range children {
		var identity struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := jsonv2.Unmarshal(raw, &identity); err != nil {
			return nil, err
		}
		used[identity.ID] = true
		names[i] = cmp.Or(identity.Name, "subagent")
		if strings.HasPrefix(names[i], "-") {
			names[i] = "subagent" + names[i]
		}
	}
	for i, base := range names {
		name := base
		for suffix := 2; used[name]; suffix++ {
			name = fmt.Sprintf("%s-%d", base, suffix)
		}
		used[name] = true
		names[i] = name
	}
	return names, nil
}

func ForSubagentType(snapshot json.RawMessage, agentType string) (json.RawMessage, error) {
	children, err := subagentSnapshots(snapshot)
	if err != nil {
		return nil, err
	}
	names, err := SubagentNames(children)
	if err != nil {
		return nil, err
	}
	for i, name := range names {
		if name == agentType {
			return children[i], nil
		}
	}
	return ForSubagent(snapshot, agentType)
}

func ForSubagent(snapshot json.RawMessage, agentID string) (json.RawMessage, error) {
	children, err := subagentSnapshots(snapshot)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		var identity rosterReference
		if err := jsonv2.Unmarshal(child, &identity); err != nil {
			return nil, err
		}
		if identity.ID == agentID {
			return child, nil
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
			if ref.ID != agent.ExternalID && len(child.Multiagent) > 0 && string(child.Multiagent) != "null" {
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

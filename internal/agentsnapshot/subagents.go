package agentsnapshot

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/agentconfig"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type rosterReference struct {
	ID      string          `json:"id"`
	Version int             `json:"version"`
	Model   json.RawMessage `json:"model"`
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
	children, err := agentconfig.Children(snapshot)
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
	children, err := agentconfig.Children(snapshot)
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
	return resolveSnapshot(ctx, database, agent.WorkspaceUUID, snapshot)
}

func ResolveSession(ctx context.Context, database *db.DB, session db.Session) (db.Session, error) {
	snapshot, err := resolveSnapshot(ctx, database, session.WorkspaceUUID, session.AgentSnapshot)
	if err != nil {
		return db.Session{}, err
	}
	if bytes.Equal(snapshot, session.AgentSnapshot) {
		return session, nil
	}
	replaced, err := database.ReplaceSessionAgentSnapshot(ctx, session.WorkspaceUUID, session.UUID, session.AgentSnapshot, snapshot)
	if err != nil {
		return db.Session{}, err
	}
	if !replaced {
		return db.Session{}, db.ErrInvalidState
	}
	session.AgentSnapshot = snapshot
	return session, nil
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

package agentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	TypeAgent              = "agent"
	TypeAgentWithOverrides = "agent_with_overrides"
)

type Config struct {
	Model      json.RawMessage
	System     *string
	Tools      json.RawMessage
	MCPServers json.RawMessage
	Skills     json.RawMessage
}

type Overrides struct {
	Model      json.RawMessage
	System     json.RawMessage
	Tools      json.RawMessage
	MCPServers json.RawMessage
	Skills     json.RawMessage
}

type SessionAgent struct {
	ID            string
	Version       int
	WithOverrides bool
	Overrides     Overrides
}

func ParseSessionAgent(raw json.RawMessage) (SessionAgent, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return SessionAgent{}, errors.New("agent is required")
	}
	var agentID string
	if json.Unmarshal(raw, &agentID) == nil {
		return sessionAgentFromID(agentID, 0, false, Overrides{})
	}
	var object struct {
		Type        string          `json:"type"`
		ID          string          `json:"id"`
		Version     *int            `json:"version"`
		Model       json.RawMessage `json:"model"`
		System      json.RawMessage `json:"system"`
		Tools       json.RawMessage `json:"tools"`
		MCPServers  json.RawMessage `json:"mcp_servers"`
		Skills      json.RawMessage `json:"skills"`
		Name        json.RawMessage `json:"name"`
		Description json.RawMessage `json:"description"`
		Metadata    json.RawMessage `json:"metadata"`
		Multiagent  json.RawMessage `json:"multiagent"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return SessionAgent{}, errors.New("agent must be a string or object")
	}
	overrides := Overrides{
		Model:      object.Model,
		System:     object.System,
		Tools:      object.Tools,
		MCPServers: object.MCPServers,
		Skills:     object.Skills,
	}
	switch object.Type {
	case "", TypeAgent:
		if hasRaw(object.Name) || hasRaw(object.Description) || hasRaw(object.Metadata) || hasRaw(object.Multiagent) || !overrides.empty() {
			return SessionAgent{}, errors.New("agent override fields require type agent_with_overrides")
		}
		return sessionAgentFromID(object.ID, derefVersion(object.Version), false, Overrides{})
	case TypeAgentWithOverrides:
		if hasRaw(object.Name) || hasRaw(object.Description) || hasRaw(object.Metadata) || hasRaw(object.Multiagent) {
			return SessionAgent{}, errors.New("agent_with_overrides cannot set name, description, metadata, or multiagent")
		}
		return sessionAgentFromID(object.ID, derefVersion(object.Version), true, overrides)
	default:
		return SessionAgent{}, errors.New("agent.type must be agent or agent_with_overrides")
	}
}

func FromAgent(agent db.Agent) Config {
	return Config{
		Model:      cloneRaw(agent.Model),
		System:     cloneString(agent.System),
		Tools:      cloneRaw(agent.Tools),
		MCPServers: cloneRaw(agent.MCPServers),
		Skills:     cloneRaw(agent.Skills),
	}
}

func WriteAgent(agent db.Agent, cfg Config) db.Agent {
	agent.Model = cloneRaw(cfg.Model)
	agent.System = cloneString(cfg.System)
	agent.Tools = cloneRaw(cfg.Tools)
	agent.MCPServers = cloneRaw(cfg.MCPServers)
	agent.Skills = cloneRaw(cfg.Skills)
	return agent
}

func Apply(base Config, overrides Overrides, allowedModelIDs []string) (Config, error) {
	next := Config{
		Model:      cloneRaw(base.Model),
		System:     cloneString(base.System),
		Tools:      cloneRaw(base.Tools),
		MCPServers: cloneRaw(base.MCPServers),
		Skills:     cloneRaw(base.Skills),
	}
	if err := applyModel(&next, overrides.Model, allowedModelIDs); err != nil {
		return Config{}, err
	}
	if err := applySystem(&next, overrides.System); err != nil {
		return Config{}, err
	}
	if err := applyLists(&next, overrides); err != nil {
		return Config{}, err
	}
	return next, nil
}

func PatchSessionSnapshot(snapshot json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return snapshot, nil
	}
	var patch struct {
		Type        json.RawMessage `json:"type"`
		ID          json.RawMessage `json:"id"`
		Version     json.RawMessage `json:"version"`
		Model       json.RawMessage `json:"model"`
		System      json.RawMessage `json:"system"`
		Skills      json.RawMessage `json:"skills"`
		Name        json.RawMessage `json:"name"`
		Description json.RawMessage `json:"description"`
		Metadata    json.RawMessage `json:"metadata"`
		Multiagent  json.RawMessage `json:"multiagent"`
		Tools       json.RawMessage `json:"tools"`
		MCPServers  json.RawMessage `json:"mcp_servers"`
	}
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, errors.New("agent must be an object")
	}
	if hasRaw(patch.Type) || hasRaw(patch.ID) || hasRaw(patch.Version) || hasRaw(patch.Model) ||
		hasRaw(patch.System) || hasRaw(patch.Skills) || hasRaw(patch.Name) || hasRaw(patch.Description) ||
		hasRaw(patch.Metadata) || hasRaw(patch.Multiagent) {
		return nil, errors.New("session agent updates may only replace tools and mcp_servers")
	}
	overrides := Overrides{Tools: patch.Tools, MCPServers: patch.MCPServers}
	if overrides.empty() {
		return snapshot, nil
	}
	base, err := configFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	next, err := Apply(base, overrides, nil)
	if err != nil {
		return nil, err
	}
	return writeConfigToSnapshot(snapshot, next)
}

func applyModel(next *Config, raw json.RawMessage, allowedModelIDs []string) error {
	if !hasRaw(raw) {
		return nil
	}
	if jsonx.IsNull(raw) {
		return errors.New("model cannot be null")
	}
	model, err := NormalizeModel(raw)
	if err != nil {
		return err
	}
	if err := ensureModelAllowed(model.ID, allowedModelIDs); err != nil {
		return err
	}
	encoded, err := jsonx.Encode(model)
	if err != nil {
		return err
	}
	next.Model = encoded
	return nil
}

func applySystem(next *Config, raw json.RawMessage) error {
	if !hasRaw(raw) {
		return nil
	}
	if jsonx.IsNull(raw) {
		next.System = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("system must be a string or null")
	}
	next.System = &value
	return nil
}

func applyLists(next *Config, overrides Overrides) error {
	mcpServers := cloneRaw(next.MCPServers)
	if hasRaw(overrides.MCPServers) {
		normalized, err := NormalizeMCPServers(overrides.MCPServers)
		if err != nil {
			return err
		}
		mcpServers = normalized
	}
	skills := cloneRaw(next.Skills)
	if hasRaw(overrides.Skills) {
		normalized, err := NormalizeSkills(overrides.Skills)
		if err != nil {
			return err
		}
		skills = normalized
	}
	tools := cloneRaw(next.Tools)
	switch {
	case hasRaw(overrides.Tools):
		normalized, err := NormalizeTools(overrides.Tools, mcpServers)
		if err != nil {
			return err
		}
		tools = normalized
	case hasRaw(overrides.MCPServers):
		normalized, err := NormalizeTools(jsonx.Default(tools, `[]`), mcpServers)
		if err != nil {
			return err
		}
		tools = normalized
	}
	if listHasEntries(skills) && !listHasEntries(tools) {
		return errors.New("skills require the read tool")
	}
	next.MCPServers = mcpServers
	next.Skills = skills
	next.Tools = tools
	return nil
}

func ensureModelAllowed(modelID string, allowedModelIDs []string) error {
	if allowedModelIDs == nil {
		return nil
	}
	for _, allowed := range allowedModelIDs {
		if allowed == modelID {
			return nil
		}
	}
	return fmt.Errorf("model %q is not configured for this workspace", modelID)
}

func sessionAgentFromID(agentID string, version int, withOverrides bool, overrides Overrides) (SessionAgent, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return SessionAgent{}, errors.New("agent id must be non-empty")
	}
	if version < 0 {
		return SessionAgent{}, errors.New("agent.version must be at least 1")
	}
	return SessionAgent{ID: agentID, Version: version, WithOverrides: withOverrides, Overrides: overrides}, nil
}

func derefVersion(version *int) int {
	if version == nil {
		return 0
	}
	if *version < 1 {
		return -1
	}
	return *version
}

func (o Overrides) empty() bool {
	return !hasRaw(o.Model) && !hasRaw(o.System) && !hasRaw(o.Tools) && !hasRaw(o.MCPServers) && !hasRaw(o.Skills)
}

func hasRaw(raw json.RawMessage) bool {
	return len(raw) > 0
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func listHasEntries(raw json.RawMessage) bool {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return false
	}
	return len(items) > 0
}

func configFromSnapshot(snapshot json.RawMessage) (Config, error) {
	if len(snapshot) == 0 || jsonx.IsNull(snapshot) {
		return Config{}, errors.New("stored session agent is invalid")
	}
	var object struct {
		Model      json.RawMessage `json:"model"`
		System     json.RawMessage `json:"system"`
		Tools      json.RawMessage `json:"tools"`
		MCPServers json.RawMessage `json:"mcp_servers"`
		Skills     json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(snapshot, &object); err != nil {
		return Config{}, errors.New("stored session agent is invalid")
	}
	system, err := systemFromRaw(object.System)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Model:      cloneRaw(object.Model),
		System:     system,
		Tools:      cloneRaw(object.Tools),
		MCPServers: cloneRaw(object.MCPServers),
		Skills:     cloneRaw(object.Skills),
	}, nil
}

func systemFromRaw(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("stored session agent is invalid")
	}
	return &value, nil
}

func writeConfigToSnapshot(snapshot json.RawMessage, cfg Config) (json.RawMessage, error) {
	var object map[string]any
	if err := json.Unmarshal(snapshot, &object); err != nil || object == nil {
		return nil, errors.New("stored session agent is invalid")
	}
	object["model"] = jsonValue(cfg.Model, map[string]any{})
	object["mcp_servers"] = jsonValue(cfg.MCPServers, []any{})
	object["skills"] = jsonValue(cfg.Skills, []any{})
	object["tools"] = jsonValue(cfg.Tools, []any{})
	if cfg.System == nil {
		object["system"] = nil
	} else {
		object["system"] = *cfg.System
	}
	return jsonx.Encode(object)
}

func jsonValue(raw json.RawMessage, fallback any) any {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return fallback
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}

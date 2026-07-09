package agentsnapshot

import (
	"encoding/json"
	"reflect"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func FromAgent(agent db.Agent) (json.RawMessage, error) {
	return httpapi.MarshalRaw(map[string]any{
		"id":          agent.ExternalID,
		"description": agent.Description,
		"mcp_servers": RawJSONValue(agent.MCPServers, []any{}),
		"metadata":    RawJSONValue(agent.Metadata, map[string]any{}),
		"model":       RawJSONValue(agent.Model, map[string]any{}),
		"multiagent":  RawJSONValue(agent.Multiagent, nil),
		"name":        agent.Name,
		"skills":      RawJSONValue(agent.Skills, []any{}),
		"system":      agent.System,
		"tools":       RawJSONValue(agent.Tools, []any{}),
		"type":        "agent",
		"version":     agent.CurrentVersion,
	})
}

func RawJSONValue(raw json.RawMessage, fallback any) any {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return fallback
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}

func SnapshotHasSkills(snapshot json.RawMessage) bool {
	raw, ok := SnapshotSkillsRaw(snapshot)
	if !ok {
		return false
	}
	return SkillsRawHasEntries(raw)
}

func SnapshotSkillsEqual(left json.RawMessage, right json.RawMessage) bool {
	leftSkills, leftOK := SnapshotSkillsRaw(left)
	rightSkills, rightOK := SnapshotSkillsRaw(right)
	if !leftOK && !rightOK {
		return true
	}
	if leftOK != rightOK {
		return false
	}
	return SameRawJSON(leftSkills, rightSkills)
}

func SnapshotSkillsRaw(snapshot json.RawMessage) (json.RawMessage, bool) {
	if len(snapshot) == 0 || httpapi.IsJSONNull(snapshot) {
		return nil, false
	}
	var object struct {
		Skills json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(snapshot, &object); err != nil {
		return nil, false
	}
	return object.Skills, true
}

func SkillsRawHasEntries(raw json.RawMessage) bool {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return false
	}
	var refs []json.RawMessage
	if err := json.Unmarshal(raw, &refs); err != nil {
		return false
	}
	return len(refs) > 0
}

func SameRawJSON(left json.RawMessage, right json.RawMessage) bool {
	if httpapi.IsJSONNull(left) && httpapi.IsJSONNull(right) {
		return true
	}
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return string(left) == string(right)
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return string(left) == string(right)
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

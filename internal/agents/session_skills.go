package agents

import "encoding/json"

func NormalizeSessionSkillsOverride(raw json.RawMessage) (json.RawMessage, error) {
	return normalizeSkills(raw)
}

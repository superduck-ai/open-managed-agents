package agentsnapshot

import (
	"encoding/json"
)

type responseToolset struct {
	Type    string            `json:"type"`
	Configs []json.RawMessage `json:"configs"`
}

type responseToolConfig struct {
	Name string          `json:"name"`
	Type json.RawMessage `json:"type"`
}

type responseAgentTools struct {
	Tools json.RawMessage `json:"tools"`
}

func ToolsForResponse(raw json.RawMessage) json.RawMessage {
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return raw
	}
	changed := false
	for i, tool := range tools {
		var item responseToolset
		if json.Unmarshal(tool, &item) != nil || item.Type != "agent_toolset_20260401" {
			continue
		}
		configsChanged := false
		for j, config := range item.Configs {
			var fields responseToolConfig
			if json.Unmarshal(config, &fields) != nil || fields.Name == "" || len(fields.Type) != 0 {
				continue
			}
			var envelope map[string]json.RawMessage
			if json.Unmarshal(config, &envelope) != nil {
				continue
			}
			envelope["type"], _ = json.Marshal(fields.Name)
			projected, err := json.Marshal(envelope)
			if err != nil {
				return raw
			}
			item.Configs[j] = projected
			configsChanged = true
		}
		if !configsChanged {
			continue
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal(tool, &envelope) != nil {
			return raw
		}
		var err error
		envelope["configs"], err = json.Marshal(item.Configs)
		if err != nil {
			return raw
		}
		tools[i], err = json.Marshal(envelope)
		if err != nil {
			return raw
		}
		changed = true
	}
	if !changed {
		return raw
	}
	result, err := json.Marshal(tools)
	if err != nil {
		return raw
	}
	return result
}

func ForResponse(raw json.RawMessage) json.RawMessage {
	var fields responseAgentTools
	if json.Unmarshal(raw, &fields) != nil || len(fields.Tools) == 0 {
		return raw
	}
	projected := ToolsForResponse(fields.Tools)
	if string(projected) == string(fields.Tools) {
		return raw
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return raw
	}
	envelope["tools"] = projected
	result, err := json.Marshal(envelope)
	if err != nil {
		return raw
	}
	return result
}

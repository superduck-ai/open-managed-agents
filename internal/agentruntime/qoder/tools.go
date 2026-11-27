package qoder

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
)

const (
	agentToolsetType     = "agent_toolset_20260401"
	deliverArtifactsName = "DeliverArtifacts"
)

type builtinToolMapping struct {
	qoderName string
	omaName   string
}

var qoderBuiltinTools = []builtinToolMapping{
	{qoderName: "Bash", omaName: "bash"},
	{qoderName: "DeliverArtifacts", omaName: "deliver_artifacts"},
	{qoderName: "Edit", omaName: "edit"},
	{qoderName: "Glob", omaName: "glob"},
	{qoderName: "Grep", omaName: "grep"},
	{qoderName: "ImageGen", omaName: "image_gen"},
	{qoderName: "ImageSearch", omaName: "image_search"},
	{qoderName: "Read", omaName: "read"},
	{qoderName: "VideoGen", omaName: "video_gen"},
	{qoderName: "WebFetch", omaName: "web_fetch"},
	{qoderName: "WebSearch", omaName: "web_search"},
	{qoderName: "Write", omaName: "write"},
}

var (
	normToQoderTool = buildNormToQoderToolMap()
	normToOMATool   = buildNormToOMAToolMap()
)

func buildNormToQoderToolMap() map[string]string {
	m := make(map[string]string, len(qoderBuiltinTools)*3)
	for _, entry := range qoderBuiltinTools {
		m[strings.ToLower(entry.qoderName)] = entry.qoderName
		m[strings.ToLower(entry.omaName)] = entry.qoderName
		m[stripUnderscoresLower(entry.omaName)] = entry.qoderName
	}
	return m
}

func buildNormToOMAToolMap() map[string]string {
	m := make(map[string]string, len(qoderBuiltinTools)*3)
	for _, entry := range qoderBuiltinTools {
		m[strings.ToLower(entry.qoderName)] = entry.omaName
		m[strings.ToLower(entry.omaName)] = entry.omaName
		m[stripUnderscoresLower(entry.omaName)] = entry.omaName
	}
	return m
}

func stripUnderscoresLower(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "_", "")
}

func toQoderToolName(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", false
	}
	if mapped, ok := normToQoderTool[strings.ToLower(trimmed)]; ok {
		return mapped, true
	}
	if mapped, ok := normToQoderTool[stripUnderscoresLower(trimmed)]; ok {
		return mapped, true
	}
	return "", false
}

func toOMAToolName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if mapped, ok := normToOMATool[strings.ToLower(trimmed)]; ok {
		return mapped
	}
	if mapped, ok := normToOMATool[stripUnderscoresLower(trimmed)]; ok {
		return mapped
	}
	return strings.ToLower(trimmed)
}

func defaultQoderToolset() map[string]json.RawMessage {
	defaultCfg, _ := json.Marshal(map[string]bool{"enabled": true})
	deliverCfg, _ := json.Marshal([]map[string]any{
		{
			"name":    deliverArtifactsName,
			"enabled": true,
		},
	})
	return map[string]json.RawMessage{
		"type":           json.RawMessage(`"agent_toolset_20260401"`),
		"default_config": json.RawMessage(defaultCfg),
		"configs":        json.RawMessage(deliverCfg),
	}
}

func TranslateToolsToQoder(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		encoded, err := json.Marshal([]map[string]json.RawMessage{defaultQoderToolset()})
		return json.RawMessage(encoded), err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, agentruntime.InvalidArgumentError("invalid tools payload", err)
	}
	if len(items) == 0 {
		encoded, err := json.Marshal([]map[string]json.RawMessage{defaultQoderToolset()})
		return json.RawMessage(encoded), err
	}
	out := make([]json.RawMessage, 0, len(items)+1)
	hasToolset := false
	for _, item := range items {
		translated, isToolset, keep, err := translateSingleToolToQoder(item)
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}
		if isToolset {
			hasToolset = true
		}
		out = append(out, translated)
	}
	if !hasToolset {
		encodedToolset, err := json.Marshal(defaultQoderToolset())
		if err != nil {
			return nil, err
		}
		out = append([]json.RawMessage{json.RawMessage(encodedToolset)}, out...)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func translateSingleToolToQoder(item json.RawMessage) (json.RawMessage, bool, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(item, &obj); err != nil {
		return nil, false, false, agentruntime.InvalidArgumentError("invalid tool entry", err)
	}
	var toolType string
	if rawType, ok := obj["type"]; ok {
		_ = json.Unmarshal(rawType, &toolType)
	}
	if toolType == "ask_user_question" || toolType == "get_port_url" {
		return nil, false, false, nil
	}
	if toolType != agentToolsetType {
		return item, false, true, nil
	}
	if err := normalizeToolsetToQoder(obj); err != nil {
		return nil, false, false, err
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, false, false, err
	}
	return json.RawMessage(encoded), true, true, nil
}

func normalizeToolsetToQoder(obj map[string]json.RawMessage) error {
	if rawEnabled, ok := obj["enabled_tools"]; ok {
		normalized, err := translateEnabledToolsToQoder(rawEnabled)
		if err != nil {
			return err
		}
		obj["enabled_tools"] = normalized
	}
	hasDeliver := false
	var configs []map[string]json.RawMessage
	if rawConfigs, ok := obj["configs"]; ok {
		var err error
		configs, hasDeliver, err = translateToolConfigsToQoder(rawConfigs)
		if err != nil {
			return err
		}
	}
	if _, hasEnabledTools := obj["enabled_tools"]; !hasEnabledTools {
		if !hasDeliver {
			deliverEntry := map[string]json.RawMessage{
				"name":    json.RawMessage(`"DeliverArtifacts"`),
				"enabled": json.RawMessage(`true`),
			}
			configs = append(configs, deliverEntry)
		}
		if len(configs) > 0 {
			encodedConfigs, err := json.Marshal(configs)
			if err != nil {
				return err
			}
			obj["configs"] = json.RawMessage(encodedConfigs)
		} else {
			delete(obj, "configs")
		}
		if _, hasDefault := obj["default_config"]; !hasDefault {
			obj["default_config"] = json.RawMessage(`{"enabled":true}`)
		}
	} else if len(configs) > 0 {
		encodedConfigs, err := json.Marshal(configs)
		if err != nil {
			return err
		}
		obj["configs"] = json.RawMessage(encodedConfigs)
	} else {
		delete(obj, "configs")
	}
	return nil
}

func translateEnabledToolsToQoder(raw json.RawMessage) (json.RawMessage, error) {
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, agentruntime.InvalidArgumentError("invalid enabled_tools list", err)
	}
	seen := make(map[string]struct{}, len(names)+1)
	out := make([]string, 0, len(names)+1)
	for _, name := range names {
		mapped, ok := toQoderToolName(name)
		if !ok {
			continue
		}
		if _, dup := seen[mapped]; dup {
			continue
		}
		seen[mapped] = struct{}{}
		out = append(out, mapped)
	}
	if _, ok := seen[deliverArtifactsName]; !ok {
		out = append(out, deliverArtifactsName)
	}
	encoded, err := json.Marshal(out)
	return json.RawMessage(encoded), err
}

func translateToolConfigsToQoder(raw json.RawMessage) ([]map[string]json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, false, agentruntime.InvalidArgumentError("invalid tool configs list", err)
	}
	out := make([]map[string]json.RawMessage, 0, len(entries))
	hasDeliver := false
	for _, entry := range entries {
		var rawName string
		if nameField, ok := entry["name"]; ok {
			_ = json.Unmarshal(nameField, &rawName)
		}
		mapped, ok := toQoderToolName(rawName)
		if !ok {
			continue
		}
		copied := make(map[string]json.RawMessage, len(entry))
		for k, v := range entry {
			if k == "type" {
				continue
			}
			copied[k] = v
		}
		encodedName, err := json.Marshal(mapped)
		if err != nil {
			return nil, false, err
		}
		copied["name"] = json.RawMessage(encodedName)
		if mapped == deliverArtifactsName {
			hasDeliver = true
			if _, hasEnabled := copied["enabled"]; !hasEnabled {
				copied["enabled"] = json.RawMessage(`true`)
			}
		}
		out = append(out, copied)
	}
	return out, hasDeliver, nil
}

func TranslateToolsFromQoder(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return raw, nil
	}
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		translated, err := translateSingleToolFromQoder(item)
		if err != nil {
			return nil, err
		}
		out = append(out, translated)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func translateSingleToolFromQoder(item json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(item, &obj); err != nil {
		return item, nil
	}
	var toolType string
	if rawType, ok := obj["type"]; ok {
		_ = json.Unmarshal(rawType, &toolType)
	}
	if toolType != agentToolsetType {
		return item, nil
	}
	if rawConfigs, ok := obj["configs"]; ok {
		normalizedConfigs, err := translateToolConfigsFromQoder(rawConfigs)
		if err != nil {
			return nil, err
		}
		obj["configs"] = normalizedConfigs
	}
	if rawEnabled, ok := obj["enabled_tools"]; ok {
		normalizedNames, err := translateEnabledToolsFromQoder(rawEnabled)
		if err != nil {
			return nil, err
		}
		encodedNames, err := json.Marshal(normalizedNames)
		if err != nil {
			return nil, err
		}
		obj["enabled_tools"] = json.RawMessage(encodedNames)
		if _, hasConfigs := obj["configs"]; !hasConfigs {
			synth := make([]map[string]any, 0, len(normalizedNames))
			for _, name := range normalizedNames {
				synth = append(synth, map[string]any{
					"name":    name,
					"enabled": true,
				})
			}
			encodedSynth, err := json.Marshal(synth)
			if err != nil {
				return nil, err
			}
			obj["configs"] = json.RawMessage(encodedSynth)
			if _, hasDefault := obj["default_config"]; !hasDefault {
				obj["default_config"] = json.RawMessage(`{"enabled":false}`)
			}
		}
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func translateToolConfigsFromQoder(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw, nil
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return raw, nil
	}
	for _, entry := range entries {
		var rawName string
		if nameField, ok := entry["name"]; ok {
			if err := json.Unmarshal(nameField, &rawName); err == nil {
				encodedName, err := json.Marshal(toOMAToolName(rawName))
				if err != nil {
					return nil, err
				}
				entry["name"] = json.RawMessage(encodedName)
			}
		}
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func translateEnabledToolsFromQoder(raw json.RawMessage) ([]string, error) {
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		mapped := toOMAToolName(name)
		if mapped == "" {
			continue
		}
		if _, dup := seen[mapped]; dup {
			continue
		}
		seen[mapped] = struct{}{}
		out = append(out, mapped)
	}
	return out, nil
}

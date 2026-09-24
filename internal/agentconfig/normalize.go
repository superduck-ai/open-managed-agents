package agentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
)

var (
	customToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	mcpNamePattern        = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

type modelInput struct {
	ID    *string `json:"id"`
	Speed *string `json:"speed"`
}

type Model struct {
	ID    string `json:"id"`
	Speed string `json:"speed"`
}

func NormalizeModel(raw json.RawMessage) (Model, error) {
	if jsonx.IsNull(raw) {
		return Model{}, errors.New("model cannot be null")
	}
	var modelID string
	if json.Unmarshal(raw, &modelID) == nil {
		if modelID == "" {
			return Model{}, errors.New("model id must be non-empty")
		}
		return Model{
			ID:    modelID,
			Speed: "standard",
		}, nil
	}
	var model modelInput
	if err := json.Unmarshal(raw, &model); err != nil {
		return Model{}, errors.New("model must be a string or object")
	}
	if model.ID == nil {
		return Model{}, errors.New("model.id is required")
	}
	modelID = *model.ID
	if modelID == "" {
		return Model{}, errors.New("model.id must be a non-empty string")
	}
	normalized := Model{
		ID:    modelID,
		Speed: "standard",
	}
	if model.Speed != nil {
		if *model.Speed != "standard" && *model.Speed != "fast" {
			return Model{}, errors.New("model.speed must be standard or fast")
		}
		normalized.Speed = *model.Speed
	}
	return normalized, nil
}

func NormalizeMCPServers(raw json.RawMessage) (json.RawMessage, error) {
	if jsonx.IsNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var servers []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, errors.New("mcp_servers must be an array")
	}
	if len(servers) > 20 {
		return nil, errors.New("mcp_servers must contain at most 20 servers")
	}
	seen := map[string]struct{}{}
	normalized := make([]map[string]string, 0, len(servers))
	for _, server := range servers {
		name, err := requiredRawString(server["name"], "mcp_servers.name")
		if err != nil {
			return nil, err
		}
		if len(name) > 255 {
			return nil, errors.New("mcp_servers.name must be at most 255 characters")
		}
		if !mcpNamePattern.MatchString(name) || strings.Contains(name, "__") {
			return nil, errors.New("mcp_servers.name must match ^[A-Za-z0-9_.-]+$ and not contain consecutive underscores")
		}
		if _, ok := seen[name]; ok {
			return nil, errors.New("mcp_servers.name must be unique")
		}
		seen[name] = struct{}{}
		serverType, err := requiredRawString(server["type"], "mcp_servers.type")
		if err != nil {
			return nil, err
		}
		if serverType != "url" {
			return nil, errors.New("mcp_servers.type must be url")
		}
		serverURL, err := requiredRawString(server["url"], "mcp_servers.url")
		if err != nil {
			return nil, err
		}
		if len(serverURL) > 2048 {
			return nil, errors.New("mcp_servers.url must be at most 2048 characters")
		}
		if !validMCPServerURL(serverURL) {
			return nil, errors.New("mcp_servers.url must be an HTTP or HTTPS absolute URL without credentials or fragment")
		}
		normalized = append(normalized, map[string]string{"name": name, "type": "url", "url": serverURL})
	}
	return jsonx.Encode(normalized)
}

func NormalizeSkills(raw json.RawMessage) (json.RawMessage, error) {
	if jsonx.IsNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var skills []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &skills); err != nil {
		return nil, errors.New("skills must be an array")
	}
	if len(skills) > 20 {
		return nil, errors.New("skills must contain at most 20 skills")
	}
	normalized := make([]map[string]string, 0, len(skills))
	for _, skill := range skills {
		skillType, err := requiredRawString(skill["type"], "skills.type")
		if err != nil {
			return nil, err
		}
		if skillType != "anthropic" && skillType != "custom" {
			return nil, errors.New("skills.type must be anthropic or custom")
		}
		skillID, err := requiredRawString(skill["skill_id"], "skills.skill_id")
		if err != nil {
			return nil, err
		}
		version := "latest"
		if rawVersion, ok := skill["version"]; ok && !jsonx.IsNull(rawVersion) {
			if err := json.Unmarshal(rawVersion, &version); err != nil || version == "" {
				return nil, errors.New("skills.version must be a non-empty string")
			}
		}
		normalized = append(normalized, map[string]string{"skill_id": skillID, "type": skillType, "version": version})
	}
	return jsonx.Encode(normalized)
}

func NormalizeTools(raw json.RawMessage, mcpServers json.RawMessage) (json.RawMessage, error) {
	if jsonx.IsNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var tools []map[string]any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, errors.New("tools must be an array")
	}
	total := 0
	serverNames, err := mcpServerNames(mcpServers)
	if err != nil {
		return nil, err
	}
	referencedMCPServers := map[string]struct{}{}
	seenToolsets := map[string]struct{}{}
	seenCustomTools := map[string]struct{}{}
	normalized := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		total++
		toolType, _ := tool["type"].(string)
		switch toolType {
		case "agent_toolset_20260401":
			if _, exists := seenToolsets[toolType]; exists {
				return nil, errors.New("agent toolset must be unique")
			}
			seenToolsets[toolType] = struct{}{}
			defaultConfig, err := normalizeDefaultConfig(tool["default_config"], "always_allow")
			if err != nil {
				return nil, err
			}
			configs, err := normalizeAgentToolConfigs(tool["configs"], permissionPolicyType(defaultConfig, "always_allow"), configEnabled(defaultConfig))
			if err != nil {
				return nil, err
			}
			total += len(configs)
			normalized = append(normalized, map[string]any{"configs": configs, "default_config": defaultConfig, "type": toolType})
		case "mcp_toolset":
			name, _ := tool["mcp_server_name"].(string)
			if name == "" {
				return nil, errors.New("mcp_toolset.mcp_server_name is required")
			}
			if len(name) > 255 || !mcpNamePattern.MatchString(name) || strings.Contains(name, "__") {
				return nil, errors.New("mcp_toolset.mcp_server_name must match ^[A-Za-z0-9_.-]+$ and not contain consecutive underscores")
			}
			if _, ok := serverNames[name]; !ok {
				return nil, errors.New("mcp_toolset.mcp_server_name must reference an MCP server")
			}
			toolsetKey := toolType + ":" + name
			if _, exists := seenToolsets[toolsetKey]; exists {
				return nil, errors.New("mcp toolset server names must be unique")
			}
			seenToolsets[toolsetKey] = struct{}{}
			referencedMCPServers[name] = struct{}{}
			defaultConfig, err := normalizeDefaultConfig(tool["default_config"], "always_ask")
			if err != nil {
				return nil, err
			}
			configs, err := normalizeMCPToolConfigs(tool["configs"], permissionPolicyType(defaultConfig, "always_ask"))
			if err != nil {
				return nil, err
			}
			total += len(configs)
			normalized = append(normalized, map[string]any{"configs": configs, "default_config": defaultConfig, "mcp_server_name": name, "type": toolType})
		case "custom":
			custom, err := normalizeCustomTool(tool)
			if err != nil {
				return nil, err
			}
			customName, _ := custom["name"].(string)
			if _, exists := seenCustomTools[customName]; exists {
				return nil, errors.New("custom tool names must be unique")
			}
			seenCustomTools[customName] = struct{}{}
			normalized = append(normalized, custom)
		default:
			return nil, errors.New("tools.type must be agent_toolset_20260401, mcp_toolset, or custom")
		}
	}
	if len(referencedMCPServers) > 0 && len(referencedMCPServers) != len(serverNames) {
		return nil, errors.New("every mcp_servers entry must be referenced by an mcp_toolset")
	}
	if total > 128 {
		return nil, errors.New("tools must contain at most 128 total tools")
	}
	return jsonx.Encode(normalized)
}

func validMCPServerURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.IsAbs() && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == ""
}

func normalizeAgentToolConfigs(value any, defaultPolicy string, defaultEnabled bool) ([]map[string]any, error) {
	var configs []map[string]any
	if value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, errors.New("tools.configs must be an array")
		}
		if err := json.Unmarshal(raw, &configs); err != nil {
			return nil, errors.New("tools.configs must be an array")
		}
	}
	allowed := map[string]struct{}{
		"task": {}, "ask_user_question": {}, "bash": {}, "cron_create": {}, "cron_delete": {}, "cron_list": {},
		"edit": {}, "enter_plan_mode": {}, "enter_worktree": {}, "exit_plan_mode": {}, "exit_worktree": {},
		"glob": {}, "grep": {}, "notebook_edit": {}, "read": {}, "schedule_wakeup": {}, "skill": {},
		"task_output": {}, "task_stop": {}, "todo_write": {}, "web_fetch": {}, "write": {},
	}
	normalized := make([]map[string]any, 0, len(configs))
	seen := map[string]struct{}{}
	for _, config := range configs {
		name, _ := config["name"].(string)
		if _, ok := allowed[name]; !ok {
			return nil, errors.New("agent tool config name is invalid")
		}
		if _, exists := seen[name]; exists {
			return nil, errors.New("agent tool config names must be unique")
		}
		seen[name] = struct{}{}
		enabled, err := boolWithDefault(config["enabled"], defaultEnabled, "tools.configs.enabled")
		if err != nil {
			return nil, err
		}
		policy, err := normalizePermissionPolicy(config["permission_policy"], defaultPolicy)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, map[string]any{"enabled": enabled, "name": name, "permission_policy": policy})
	}
	if _, configured := seen["ask_user_question"]; !configured {
		normalized = append(normalized, map[string]any{
			"enabled":           false,
			"name":              "ask_user_question",
			"permission_policy": map[string]string{"type": "always_allow"},
		})
	}
	return normalized, nil
}

func configEnabled(defaultConfig map[string]any) bool {
	enabled, ok := defaultConfig["enabled"].(bool)
	return !ok || enabled
}

func normalizeMCPToolConfigs(value any, defaultPolicy string) ([]map[string]any, error) {
	if value == nil {
		return []map[string]any{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("tools.configs must be an array")
	}
	var configs []map[string]any
	if err := json.Unmarshal(raw, &configs); err != nil {
		return nil, errors.New("tools.configs must be an array")
	}
	normalized := make([]map[string]any, 0, len(configs))
	seen := map[string]struct{}{}
	for _, config := range configs {
		name, _ := config["name"].(string)
		if !mcpNamePattern.MatchString(name) || len(name) > 128 {
			return nil, errors.New("mcp tool config name must match ^[A-Za-z0-9_.-]{1,128}$")
		}
		if _, exists := seen[name]; exists {
			return nil, errors.New("mcp tool config names must be unique")
		}
		seen[name] = struct{}{}
		enabled, err := boolWithDefault(config["enabled"], true, "tools.configs.enabled")
		if err != nil {
			return nil, err
		}
		policy, err := normalizePermissionPolicy(config["permission_policy"], defaultPolicy)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, map[string]any{"enabled": enabled, "name": name, "permission_policy": policy})
	}
	return normalized, nil
}

func normalizeDefaultConfig(value any, defaultPolicy string) (map[string]any, error) {
	if value == nil {
		return map[string]any{"enabled": true, "permission_policy": map[string]string{"type": defaultPolicy}}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("default_config must be an object")
	}
	enabled, err := boolWithDefault(object["enabled"], true, "default_config.enabled")
	if err != nil {
		return nil, err
	}
	policy, err := normalizePermissionPolicy(object["permission_policy"], defaultPolicy)
	if err != nil {
		return nil, err
	}
	return map[string]any{"enabled": enabled, "permission_policy": policy}, nil
}

func normalizePermissionPolicy(value any, defaultPolicy string) (map[string]string, error) {
	if value == nil {
		return map[string]string{"type": defaultPolicy}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("permission_policy must be an object")
	}
	policyType, _ := object["type"].(string)
	if policyType != "always_allow" && policyType != "always_ask" {
		return nil, errors.New("permission_policy.type must be always_allow or always_ask")
	}
	return map[string]string{"type": policyType}, nil
}

func permissionPolicyType(config map[string]any, fallback string) string {
	policy, _ := config["permission_policy"].(map[string]string)
	if policy == nil {
		return fallback
	}
	if policy["type"] == "" {
		return fallback
	}
	return policy["type"]
}

func normalizeCustomTool(tool map[string]any) (map[string]any, error) {
	name, _ := tool["name"].(string)
	if !customToolNamePattern.MatchString(name) {
		return nil, errors.New("custom tool name must match ^[A-Za-z0-9_-]{1,128}$")
	}
	description, _ := tool["description"].(string)
	if len(description) < 1 || len(description) > 1024 {
		return nil, errors.New("custom tool description must be between 1 and 1024 characters")
	}
	schema, ok := tool["input_schema"].(map[string]any)
	if !ok {
		return nil, errors.New("custom tool input_schema must be an object")
	}
	schemaType, _ := schema["type"].(string)
	if schemaType != "object" {
		return nil, errors.New("custom tool input_schema.type must be object")
	}
	return map[string]any{"description": description, "input_schema": schema, "name": name, "type": "custom"}, nil
}

func mcpServerNames(raw json.RawMessage) (map[string]struct{}, error) {
	var servers []struct {
		Name string `json:"name"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, errors.New("stored mcp_servers are invalid")
		}
	}
	names := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		names[server.Name] = struct{}{}
	}
	return names, nil
}

func requiredRawString(raw json.RawMessage, name string) (string, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return "", fmt.Errorf("%s is required", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return value, nil
}

func boolWithDefault(value any, fallback bool, name string) (bool, error) {
	if value == nil {
		return fallback, nil
	}
	if parsed, ok := value.(bool); ok {
		return parsed, nil
	}
	return false, fmt.Errorf("%s must be a boolean", name)
}

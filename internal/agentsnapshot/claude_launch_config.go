package agentsnapshot

import (
	"encoding/json"
	"strings"
)

type ClaudeLaunchConfig struct {
	Tools        string `json:"tools"`
	AllowedTools string `json:"allowed_tools,omitempty"`
}

type claudeLaunchSnapshot struct {
	Tools []claudeToolset `json:"tools"`
}

type claudeToolset struct {
	Type          string                 `json:"type"`
	MCPServerName string                 `json:"mcp_server_name"`
	DefaultConfig claudePermissionConfig `json:"default_config"`
	Configs       []claudeToolConfig     `json:"configs"`
}

type claudeToolConfig struct {
	Name             string                  `json:"name"`
	Enabled          *bool                   `json:"enabled"`
	PermissionPolicy *claudePermissionPolicy `json:"permission_policy"`
}

type claudePermissionConfig struct {
	Enabled          *bool                   `json:"enabled"`
	PermissionPolicy *claudePermissionPolicy `json:"permission_policy"`
}

type claudePermissionPolicy struct {
	Type string `json:"type"`
}

type claudeBuiltInTool struct {
	configName string
	claudeName string
}

// 与当前固定的 Claude Code 2.1.120 system/init 工具集合保持一致，仅移除 WebSearch；原有 7 项优先输出。
var claudeBuiltInTools = []claudeBuiltInTool{
	{configName: "bash", claudeName: "Bash"},
	{configName: "read", claudeName: "Read"},
	{configName: "write", claudeName: "Write"},
	{configName: "edit", claudeName: "Edit"},
	{configName: "glob", claudeName: "Glob"},
	{configName: "grep", claudeName: "Grep"},
	{configName: "web_fetch", claudeName: "WebFetch"},
	{configName: "task", claudeName: "Task"},
	{configName: "ask_user_question", claudeName: "AskUserQuestion"},
	{configName: "cron_create", claudeName: "CronCreate"},
	{configName: "cron_delete", claudeName: "CronDelete"},
	{configName: "cron_list", claudeName: "CronList"},
	{configName: "enter_plan_mode", claudeName: "EnterPlanMode"},
	{configName: "enter_worktree", claudeName: "EnterWorktree"},
	{configName: "exit_plan_mode", claudeName: "ExitPlanMode"},
	{configName: "exit_worktree", claudeName: "ExitWorktree"},
	{configName: "notebook_edit", claudeName: "NotebookEdit"},
	{configName: "schedule_wakeup", claudeName: "ScheduleWakeup"},
	{configName: "skill", claudeName: "Skill"},
	{configName: "task_output", claudeName: "TaskOutput"},
	{configName: "task_stop", claudeName: "TaskStop"},
	{configName: "todo_write", claudeName: "TodoWrite"},
}

func ClaudeLaunchConfigFromSnapshot(snapshot json.RawMessage) (ClaudeLaunchConfig, error) {
	var value claudeLaunchSnapshot
	if len(snapshot) > 0 && strings.TrimSpace(string(snapshot)) != "null" {
		if err := json.Unmarshal(snapshot, &value); err != nil {
			return ClaudeLaunchConfig{}, err
		}
	}
	return ClaudeLaunchConfig{
		Tools:        claudeBuiltInToolNames(),
		AllowedTools: claudeAllowedTools(value.Tools),
	}, nil
}

func claudeBuiltInToolNames() string {
	names := make([]string, 0, len(claudeBuiltInTools))
	for _, tool := range claudeBuiltInTools {
		names = append(names, tool.claudeName)
	}
	return strings.Join(names, ",")
}

func claudeAllowedTools(toolsets []claudeToolset) string {
	allowed := make([]string, 0, len(claudeBuiltInTools))
	for _, toolset := range toolsets {
		switch toolset.Type {
		case "agent_toolset_20260401":
			allowed = append(allowed, claudeAllowedBuiltInTools(toolset)...)
		case "mcp_toolset":
			allowed = append(allowed, claudeAllowedMCPTools(toolset)...)
		}
	}
	return strings.Join(allowed, ",")
}

func claudeAllowedBuiltInTools(toolset claudeToolset) []string {
	defaultAllowed := claudeToolConfigAllows(toolset.DefaultConfig, true, "always_allow")
	defaultPolicy := claudeToolConfigPolicy(toolset.DefaultConfig, "always_allow")
	configs := claudeToolConfigsByName(toolset.Configs)
	allowed := make([]string, 0, len(claudeBuiltInTools))
	for _, tool := range claudeBuiltInTools {
		config, configured := configs[tool.configName]
		if (!configured && defaultAllowed) || (configured && claudeToolConfigAllows(config.permissionConfig(), true, defaultPolicy)) {
			allowed = append(allowed, tool.claudeName)
		}
	}
	return allowed
}

func claudeAllowedMCPTools(toolset claudeToolset) []string {
	if !safeClaudeMCPServerRuleComponent(toolset.MCPServerName) {
		return nil
	}
	defaultAllowed := claudeToolConfigAllows(toolset.DefaultConfig, true, "always_ask")
	defaultPolicy := claudeToolConfigPolicy(toolset.DefaultConfig, "always_ask")
	configs := claudeToolConfigsByName(toolset.Configs)
	allOverridesAllowed := true
	for _, config := range configs {
		if !claudeToolConfigAllows(config.permissionConfig(), true, defaultPolicy) {
			allOverridesAllowed = false
			break
		}
	}
	if defaultAllowed && allOverridesAllowed {
		return []string{"mcp__" + toolset.MCPServerName + "__*"}
	}
	allowed := make([]string, 0, len(configs))
	for _, config := range toolset.Configs {
		if _, authoritative := configs[config.Name]; !authoritative {
			continue
		}
		delete(configs, config.Name)
		if safeClaudeToolRuleComponent(config.Name) && claudeToolConfigAllows(config.permissionConfig(), true, defaultPolicy) {
			allowed = append(allowed, "mcp__"+toolset.MCPServerName+"__"+config.Name)
		}
	}
	return allowed
}

func claudeToolConfigsByName(configs []claudeToolConfig) map[string]claudeToolConfig {
	byName := make(map[string]claudeToolConfig, len(configs))
	for _, config := range configs {
		if config.Name != "" {
			if _, exists := byName[config.Name]; !exists {
				byName[config.Name] = config
			}
		}
	}
	return byName
}

func (config claudeToolConfig) permissionConfig() claudePermissionConfig {
	return claudePermissionConfig{Enabled: config.Enabled, PermissionPolicy: config.PermissionPolicy}
}

func claudeToolConfigAllows(config claudePermissionConfig, fallbackEnabled bool, fallbackPolicy string) bool {
	enabled := fallbackEnabled
	if config.Enabled != nil {
		enabled = *config.Enabled
	}
	return enabled && claudeToolConfigPolicy(config, fallbackPolicy) == "always_allow"
}

func claudeToolConfigPolicy(config claudePermissionConfig, fallback string) string {
	if config.PermissionPolicy != nil && config.PermissionPolicy.Type != "" {
		return config.PermissionPolicy.Type
	}
	return fallback
}

func safeClaudeMCPServerRuleComponent(value string) bool {
	return !strings.Contains(value, "__") && safeClaudeToolRuleComponent(value)
}

func safeClaudeToolRuleComponent(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}

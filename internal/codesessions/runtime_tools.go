package codesessions

import (
	"encoding/json"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

var sandboxToolNames = []string{"Bash", "Read", "Write", "Edit", "Grep", "Glob", "web_search", "web_fetch", "ask_user_question", sessionports.ToolName}

type builtinToolCatalog struct {
	portURL bool
}

func builtinToolCatalogFor(settings *sessionports.Settings) builtinToolCatalog {
	return builtinToolCatalog{portURL: settings.Enabled()}
}

func (c builtinToolCatalog) names() []string {
	if c.portURL {
		return sandboxToolNames
	}
	return slices.DeleteFunc(slices.Clone(sandboxToolNames), func(name string) bool {
		return name == sessionports.ToolName
	})
}

func (c builtinToolCatalog) available(claudeToolName string) bool {
	return c.portURL || managedAgentToolName(claudeToolName) != sessionports.ToolName
}

func (c builtinToolCatalog) enabledSandboxTools(tools []toolPermissionToolset) []string {
	names := c.names()
	enabled := make([]string, 0, len(names))
	for _, name := range names {
		if resolveAgentToolPermission(tools, managedAgentToolName(name)) != resolvedToolPermissionDeny {
			enabled = append(enabled, name)
		}
	}
	return enabled
}

func (c builtinToolCatalog) runtimeTools(tools []toolPermissionToolset) []string {
	if !slices.ContainsFunc(tools, func(toolset toolPermissionToolset) bool {
		return toolset.Type == "agent_toolset_20260401"
	}) {
		return make([]string, 0)
	}
	return c.enabledSandboxTools(tools)
}

func (c builtinToolCatalog) toolPolicies(tools []toolPermissionToolset) map[string]string {
	names := c.names()
	policies := make(map[string]string, len(names))
	for _, name := range names {
		remoteName := managedAgentToolName(name)
		permission := resolveAgentToolPermission(tools, remoteName)
		if permission == resolvedToolPermissionAuto {
			permission = resolvedToolPermissionAsk
		}
		policies[remoteName] = string(permission)
	}
	return policies
}

func (c builtinToolCatalog) toolEnabled(snapshot json.RawMessage, name string) (bool, error) {
	tools, err := runtimeRootToolsets(snapshot)
	if err != nil {
		return false, err
	}
	permission, _ := resolveToolPermissionFromAgentSnapshot(snapshot, name)
	return permission != resolvedToolPermissionDeny && slices.Contains(c.runtimeTools(tools), name), nil
}

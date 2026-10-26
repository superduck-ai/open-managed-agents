package codesessions

func runtimeBuiltinTools(tools []toolPermissionToolset) []string {
	result := make([]string, 0)
	for _, toolset := range tools {
		if toolset.Type != "agent_toolset_20260401" {
			continue
		}
		for _, name := range sandboxToolNames {
			if resolveAgentToolPermission(tools, managedAgentToolName(name)) != resolvedToolPermissionDeny {
				result = append(result, name)
			}
		}
		break
	}
	return result
}

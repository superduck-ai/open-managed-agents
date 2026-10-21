package codesessions

func runtimeBuiltinTools(tools []toolPermissionToolset) []string {
	result := make([]string, 0)
	for _, toolset := range tools {
		if toolset.Type != "agent_toolset_20260401" {
			continue
		}
		for _, name := range []string{"Bash", "Read", "Write", "Edit", "Grep", "Glob"} {
			if resolveAgentToolPermission(tools, managedAgentToolName(name)) != resolvedToolPermissionDeny {
				result = append(result, name)
			}
		}
		break
	}
	return result
}

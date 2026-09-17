package codesessions

// Toolset declarations are parsed at the Agent/Code Session JSON boundary.
type mcpToolsetDeclaration struct {
	Type       string               `json:"type"`
	ServerName string               `json:"mcp_server_name"`
	Configs    []mcpToolDeclaration `json:"configs"`
}

type mcpToolDeclaration struct {
	Name             string `json:"name"`
	Enabled          *bool  `json:"enabled"`
	PermissionPolicy struct {
		Type string `json:"type"`
	} `json:"permission_policy"`
}

type mcpRuntimeTool struct {
	Name             string `json:"name"`
	Enabled          *bool  `json:"enabled,omitempty"`
	PermissionPolicy string `json:"permission_policy,omitempty"`
}

func mcpRuntimeToolsets(declarations []mcpToolsetDeclaration) map[string][]mcpRuntimeTool {
	toolsets := make(map[string][]mcpRuntimeTool)
	for _, declaration := range declarations {
		if declaration.Type != "mcp_toolset" || declaration.ServerName == "" {
			continue
		}
		tools := make([]mcpRuntimeTool, 0, len(declaration.Configs))
		for _, tool := range declaration.Configs {
			if tool.Name == "" {
				continue
			}
			config := mcpRuntimeTool{Name: tool.Name, Enabled: tool.Enabled}
			switch tool.PermissionPolicy.Type {
			case "always_allow", "allow":
				config.PermissionPolicy = "always_allow"
			case "always_ask", "ask":
				config.PermissionPolicy = "always_ask"
			}
			tools = append(tools, config)
		}
		toolsets[declaration.ServerName] = tools
	}
	return toolsets
}

package agent

import "github.com/charmbracelet/crush/internal/agent/tools/mcp"

func (a *sessionAgent) mcpStates() map[string]mcp.ClientInfo {
	if a.isolatedMCP {
		return nil
	}
	return mcp.GetStates()
}

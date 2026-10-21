package codesessions

import "encoding/json"

func isDelegationTool(name string) bool {
	return name == "list_agents" || name == "send_to_agent" || name == "archive_agent"
}

func delegationToolPermission(raw json.RawMessage) resolvedToolPermission {
	var config struct {
		Type      string             `json:"type"`
		Agents    []json.RawMessage  `json:"agents"`
		Subagents *runtimeDelegation `json:"subagents"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return resolvedToolPermissionDeny
	}
	switch config.Type {
	case "coordinator":
		if len(config.Agents) > 0 {
			return resolvedToolPermissionAllow
		}
	case "multiagent_20261001":
		if config.Subagents == nil || config.Subagents.Type == "enabled" {
			return resolvedToolPermissionAllow
		}
	}
	return resolvedToolPermissionDeny
}

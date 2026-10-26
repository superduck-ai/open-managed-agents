package agents

import "encoding/json"

func NormalizeSessionMCPServersOverride(raw json.RawMessage) (json.RawMessage, error) {
	return normalizeMCPServers(raw)
}

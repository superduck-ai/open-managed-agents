package mcpcatalogs

import (
	"encoding/json"
	"strings"
)

type AgentServer struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

// ParseAgentServers 将 Agent 持久化的 MCP 配置解析为详情页目录查询所需的最小结构。
// 字段在这里统一去除首尾空白，避免读取目录和手动刷新使用不同的 endpoint identity。
func ParseAgentServers(raw json.RawMessage) ([]AgentServer, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []AgentServer{}, nil
	}
	var servers []AgentServer
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, err
	}
	for index := range servers {
		servers[index].Name = strings.TrimSpace(servers[index].Name)
		servers[index].Type = strings.TrimSpace(servers[index].Type)
		servers[index].URL = strings.TrimSpace(servers[index].URL)
	}
	return servers, nil
}

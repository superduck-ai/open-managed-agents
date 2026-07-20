package networkpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	urlpkg "net/url"
	"strings"
)

// ErrMalformedAgentSnapshot 表示 Session AgentSnapshot 或其中的 MCP URL
// 无法安全解析；allow_mcp_servers 开启时调用方必须 fail closed。
var ErrMalformedAgentSnapshot = errors.New("malformed session agent snapshot")

// MCPAllowedHosts 从 Session AgentSnapshot 的 mcp_servers[].url 现场提取
// 已归一化的 hostname 列表，去重且顺序稳定。stdio 类 server 没有 URL，
// 自然排除；畸形 snapshot 或非空但不可解析的 URL 返回错误。runner 写入 work
// metadata 与 proxy 授权共用本函数，保证两处语义不漂移。
func MCPAllowedHosts(agentSnapshot json.RawMessage) ([]string, error) {
	var snapshot struct {
		Servers []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcp_servers"`
	}
	if len(agentSnapshot) == 0 {
		return nil, ErrMalformedAgentSnapshot
	}
	if err := json.Unmarshal(agentSnapshot, &snapshot); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON", ErrMalformedAgentSnapshot)
	}
	var hosts []string
	for _, server := range snapshot.Servers {
		serverType := strings.ToLower(strings.TrimSpace(server.Type))
		rawURL := strings.TrimSpace(server.URL)
		if serverType == "stdio" && rawURL == "" {
			continue
		}
		if rawURL == "" || (serverType != "url" && serverType != "http" && serverType != "sse") {
			return nil, fmt.Errorf("%w: invalid MCP server contract", ErrMalformedAgentSnapshot)
		}
		parsed, err := urlpkg.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid MCP server URL", ErrMalformedAgentSnapshot)
		}
		scheme := strings.ToLower(parsed.Scheme)
		if !parsed.IsAbs() || parsed.Host == "" || (scheme != "http" && scheme != "https") {
			return nil, fmt.Errorf("%w: invalid MCP server URL", ErrMalformedAgentSnapshot)
		}
		host, err := NormalizeHost(parsed.Hostname())
		if err != nil || !validNormalizedHostname(host) {
			return nil, fmt.Errorf("%w: invalid MCP server URL", ErrMalformedAgentSnapshot)
		}
		hosts = append(hosts, host)
	}
	return DedupeStrings(hosts), nil
}

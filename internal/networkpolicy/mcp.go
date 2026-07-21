package networkpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	urlpkg "net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/common/collections"
)

// ErrMalformedAgentSnapshot 表示 Session AgentSnapshot 或其中的 MCP URL
// 无法安全解析；allow_mcp_servers 开启时调用方必须 fail closed。
var ErrMalformedAgentSnapshot = errors.New("malformed session agent snapshot")

type agentSnapshotSchema struct {
	MCPServers mcpServerListSchema `json:"mcp_servers"`
}

type mcpServerSchema struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type mcpServerListSchema []mcpServerSchema

func (s *mcpServerListSchema) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%w: mcp_servers must not be null", ErrMalformedAgentSnapshot)
	}
	var servers []mcpServerSchema
	if err := json.Unmarshal(raw, &servers); err != nil {
		return err
	}
	*s = servers
	return nil
}

// MCPAllowedHosts 从 Session AgentSnapshot 的 mcp_servers[].url 现场提取
// 已归一化的 hostname 列表，去重且顺序稳定。stdio 类 server 没有 URL，
// 自然排除；畸形 snapshot 或非空但不可解析的 URL 返回错误。runner 写入 work
// metadata 与 proxy 授权共用本函数，保证两处语义不漂移。
func MCPAllowedHosts(agentSnapshot json.RawMessage) ([]string, error) {
	var snapshot agentSnapshotSchema
	trimmed := bytes.TrimSpace(agentSnapshot)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, ErrMalformedAgentSnapshot
	}
	if err := json.Unmarshal(trimmed, &snapshot); err != nil {
		if errors.Is(err, ErrMalformedAgentSnapshot) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: invalid JSON", ErrMalformedAgentSnapshot)
	}
	var hosts []string
	for _, server := range snapshot.MCPServers {
		host, err := mcpServerHost(server)
		if err != nil {
			return nil, err
		}
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return collections.UniqueTrimmedStrings(hosts), nil
}

func mcpServerHost(server mcpServerSchema) (string, error) {
	serverType := strings.ToLower(strings.TrimSpace(server.Type))
	rawURL := strings.TrimSpace(server.URL)
	if serverType == "stdio" {
		if rawURL != "" {
			return "", fmt.Errorf("%w: server type %q must not include a URL", ErrMalformedAgentSnapshot, serverType)
		}
		return "", nil
	}
	if serverType != "url" && serverType != "http" && serverType != "sse" {
		return "", fmt.Errorf("%w: unsupported MCP server type %q", ErrMalformedAgentSnapshot, serverType)
	}
	if rawURL == "" {
		return "", fmt.Errorf("%w: server type %q requires a URL", ErrMalformedAgentSnapshot, serverType)
	}
	parsed, err := urlpkg.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("%w: server type %q has an invalid URL", ErrMalformedAgentSnapshot, serverType)
	}
	if !parsed.IsAbs() {
		return "", fmt.Errorf("%w: MCP server URL must be absolute", ErrMalformedAgentSnapshot)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: MCP server URL must use http or https", ErrMalformedAgentSnapshot)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%w: MCP server URL must include a host", ErrMalformedAgentSnapshot)
	}
	host, err := NormalizeHost(parsed.Hostname())
	if err != nil {
		return "", fmt.Errorf("%w: MCP server URL has an invalid host", ErrMalformedAgentSnapshot)
	}
	return host, nil
}

package environments

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	urlpkg "net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
)

const managedAgentMCPConfigPath = "/tmp/managed-agent-mcp-config.json"

type managedAgentMCPDocument struct {
	Servers map[string]managedAgentMCPServerConfig `json:"mcpServers"`
}

type managedAgentMCPServerConfig struct {
	Type  string                      `json:"type"`
	URL   string                      `json:"url"`
	Tools []managedAgentMCPToolConfig `json:"tools,omitempty"`
}

type managedAgentMCPToolConfig struct {
	Name             string `json:"name"`
	Enabled          *bool  `json:"enabled,omitempty"`
	PermissionPolicy string `json:"permission_policy,omitempty"`
}

type managedAgentMCPConfigFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    int    `json:"mode"`
}

// These fields belong to the environment-manager startup JSON boundary.
// RawMessage retains unknown configuration and exact JSON numbers.
type managedAgentMCPStartupFields struct {
	MCPConfig      json.RawMessage            `json:"mcp_config"`
	MCPConfigFile  managedAgentMCPConfigFile  `json:"mcp_config_file"`
	ClaudeCodeArgs map[string]json.RawMessage `json:"claude_code_args"`
}

type managedAgentMCPProjectionInput struct {
	MCPConfig      json.RawMessage               `json:"mcp_config"`
	MCPServers     []managedAgentMCPServerTarget `json:"mcp_servers"`
	ClaudeCodeArgs map[string]json.RawMessage    `json:"claude_code_args"`
}

type managedAgentMCPServerTarget struct {
	URL string `json:"url"`
}

type managedAgentMCPRuntimeServers struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type managedAgentMCPTunnelTarget struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

func managedAgentMCPConfig(mcpServers []any, tools []any) *managedAgentMCPDocument {
	toolsets := mcpToolsetsByServer(tools)
	servers := make(map[string]managedAgentMCPServerConfig)
	for _, value := range mcpServers {
		server, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name := stringFromMap(server, "name")
		serverURL := stringFromMap(server, "url")
		if name == "" || serverURL == "" {
			continue
		}
		config := managedAgentMCPServerConfig{
			Type: mcpServerTransportType(stringFromMap(server, "type"), serverURL),
			URL:  serverURL,
		}
		if toolset, ok := toolsets[name]; ok {
			if toolConfigs := mcpServerToolConfigs(toolset["configs"]); len(toolConfigs) > 0 {
				config.Tools = toolConfigs
			}
		}
		servers[name] = config
	}
	if len(servers) == 0 {
		return nil
	}
	return &managedAgentMCPDocument{Servers: servers}
}

// managedAgentMCPConfigFields encodes the MCP document and its launch fields.
// It only handles JSON serialization; it never issues credentials or starts a process.
func managedAgentMCPConfigFields(document any, args map[string]json.RawMessage) (managedAgentMCPStartupFields, error) {
	content, err := json.Marshal(document)
	if err != nil {
		return managedAgentMCPStartupFields{}, fmt.Errorf("encode managed-agent MCP config: %w", err)
	}
	if args == nil {
		args = make(map[string]json.RawMessage)
	}
	args["mcp-config"], _ = json.Marshal(managedAgentMCPConfigPath)
	return managedAgentMCPStartupFields{
		MCPConfig:      content,
		MCPConfigFile:  managedAgentMCPConfigFile{Path: managedAgentMCPConfigPath, Content: base64.StdEncoding.EncodeToString(content), Mode: 0o600},
		ClaudeCodeArgs: args,
	}, nil
}

// projectManagedAgentRuntimeMCPConfig builds a fresh startup configuration using
// the current Code Session identity. The persisted source remains unchanged.
func projectManagedAgentRuntimeMCPConfig(sessionConfig json.RawMessage, codeSessionID, sessionIngressToken string, cfg config.Config) (json.RawMessage, error) {
	var input managedAgentMCPProjectionInput
	if len(sessionConfig) > 0 {
		if err := json.Unmarshal(sessionConfig, &input); err != nil {
			return nil, err
		}
	}
	var document managedAgentMCPRuntimeServers
	if len(input.MCPConfig) > 0 {
		if err := json.Unmarshal(input.MCPConfig, &document); err != nil {
			return nil, err
		}
	}
	if len(document.Servers) == 0 {
		hasTunnel, err := managedAgentMCPServerListHasTunnel(input.MCPServers, cfg.Tunnel)
		if err != nil {
			return nil, err
		}
		if hasTunnel {
			return nil, errManagedAgentMCPConfigMissing
		}
		return sessionConfig, nil
	}
	names, err := managedAgentTunnelServerNames(document.Servers, cfg.Tunnel)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return sessionConfig, nil
	}
	apiBaseURL := codeSessionSandboxAPIBaseURL(cfg)
	if apiBaseURL == "" {
		return nil, errManagedAgentMCPGatewayMissing
	}
	if codeSessionID == "" || sessionIngressToken == "" {
		return nil, errManagedAgentMCPIdentityMissing
	}
	for _, name := range names {
		target := managedAgentMCPTunnelTarget{
			URL:     apiBaseURL + "/v2/ccr-sessions/" + urlpkg.PathEscape(codeSessionID) + "/mcp/" + urlpkg.PathEscape(name),
			Headers: map[string]string{"Authorization": "Bearer " + sessionIngressToken},
		}
		document.Servers[name], err = mergeManagedAgentMCPFields(document.Servers[name], target)
		if err != nil {
			return nil, err
		}
	}
	configJSON, err := mergeManagedAgentMCPFields(input.MCPConfig, document)
	if err != nil {
		return nil, err
	}
	fields, err := managedAgentMCPConfigFields(configJSON, input.ClaudeCodeArgs)
	if err != nil {
		return nil, err
	}
	return mergeManagedAgentMCPFields(sessionConfig, fields)
}

// mergeManagedAgentMCPFields overlays known fields at a JSON boundary, retaining
// unrelated fields as opaque JSON rather than decoding them through float64.
func mergeManagedAgentMCPFields(original json.RawMessage, patch any) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(original, &fields); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func managedAgentTunnelServerNames(servers map[string]json.RawMessage, cfg config.TunnelConfig) ([]string, error) {
	names := make([]string, 0, len(servers))
	for name, raw := range servers {
		var target managedAgentMCPServerTarget
		if err := json.Unmarshal(raw, &target); err != nil {
			return nil, err
		}
		recognized, err := managedAgentMCPURLIsTunnel(strings.TrimSpace(target.URL), cfg)
		if err != nil {
			return nil, err
		}
		if recognized {
			names = append(names, name)
		}
	}
	return names, nil
}

func managedAgentMCPServerListHasTunnel(servers []managedAgentMCPServerTarget, cfg config.TunnelConfig) (bool, error) {
	for _, server := range servers {
		recognized, err := managedAgentMCPURLIsTunnel(strings.TrimSpace(server.URL), cfg)
		if err != nil {
			return false, err
		}
		if recognized {
			return true, nil
		}
	}
	return false, nil
}

func managedAgentMCPURLIsTunnel(rawURL string, cfg config.TunnelConfig) (bool, error) {
	if rawURL == "" {
		return false, nil
	}
	target, err := urlpkg.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("parse managed-agent MCP server URL: %w", err)
	}
	_, recognized, err := tunnels.RecognizeTarget(target, cfg)
	return recognized, err
}

func mcpToolsetsByServer(tools []any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok || stringFromMap(tool, "type") != "mcp_toolset" {
			continue
		}
		serverName := stringFromMap(tool, "mcp_server_name")
		if serverName == "" {
			continue
		}
		out[serverName] = tool
	}
	return out
}

func mcpServerToolConfigs(value any) []managedAgentMCPToolConfig {
	configs := arrayValue(value)
	out := make([]managedAgentMCPToolConfig, 0, len(configs))
	for _, item := range configs {
		config, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringFromMap(config, "name")
		if name == "" {
			continue
		}
		tool := managedAgentMCPToolConfig{Name: name}
		if enabled, ok := config["enabled"].(bool); ok {
			tool.Enabled = &enabled
		}
		if policy := mcpPermissionPolicy(config["permission_policy"]); policy != "" {
			tool.PermissionPolicy = policy
		}
		out = append(out, tool)
	}
	return out
}

func mcpPermissionPolicy(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	switch stringFromMap(object, "type") {
	case "always_allow", "allow":
		return "always_allow"
	case "always_ask", "ask":
		return "always_ask"
	default:
		return ""
	}
}

func mcpServerTransportType(serverType string, rawURL string) string {
	switch strings.TrimSpace(strings.ToLower(serverType)) {
	case "sse":
		return "sse"
	case "http", "ws":
		return strings.TrimSpace(strings.ToLower(serverType))
	case "websocket":
		return "ws"
	}
	parsed, err := urlpkg.Parse(strings.TrimSpace(rawURL))
	if err == nil && strings.HasSuffix(strings.TrimRight(strings.ToLower(parsed.Path), "/"), "/sse") {
		return "sse"
	}
	return "http"
}

package codesessions

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
)

// MCPRuntimeIdentity is provided by an authenticated runtime boundary, never by
// the persisted MCP declaration. It is shared by launch and session_context.
type MCPRuntimeIdentity struct {
	CodeSessionID       string
	SessionIngressToken string
	APIBaseURL          string
}

type mcpRuntimeInput struct {
	MCPServers json.RawMessage         `json:"mcp_servers"`
	Tools      []mcpToolsetDeclaration `json:"tools"`
	MCPConfig  json.RawMessage         `json:"mcp_config"`
}

type mcpServerDeclaration struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type mcpRuntimeDocument struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type mcpRuntimeServer interface {
	build(MCPRuntimeIdentity) (json.RawMessage, error)
}

type remoteMCPServer struct {
	document    json.RawMessage
	fields      map[string]json.RawMessage
	declaration *mcpServerDeclaration
}

type tunnelMCPServer struct {
	name   string
	fields map[string]json.RawMessage
}

type mcpClientTarget struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// BuildMCPRuntimeConfig resolves source declarations into separate remote and
// Tunnel builders. Tunnel entries never pass through remote transport inference.
// Only the returned document contains the current runtime credential.
func BuildMCPRuntimeConfig(source json.RawMessage, identity MCPRuntimeIdentity, cfg config.TunnelConfig) (json.RawMessage, error) {
	var input mcpRuntimeInput
	if len(source) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(source, &input); err != nil {
		return nil, err
	}
	var document mcpRuntimeDocument
	if len(input.MCPConfig) > 0 {
		if err := json.Unmarshal(input.MCPConfig, &document); err != nil {
			return nil, err
		}
	}
	servers, err := parseMCPRuntimeServers(input, document.Servers, cfg)
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		if string(input.MCPConfig) == "null" {
			return nil, nil
		}
		return input.MCPConfig, nil
	}
	document.Servers = make(map[string]json.RawMessage, len(servers))
	for name, server := range servers {
		encoded, err := server.build(identity)
		if err != nil {
			return nil, err
		}
		document.Servers[name] = encoded
	}
	fields := make(map[string]json.RawMessage)
	if len(input.MCPConfig) > 0 {
		if err := json.Unmarshal(input.MCPConfig, &fields); err != nil {
			return nil, err
		}
	}
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	fields["mcpServers"], err = json.Marshal(document.Servers)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func parseMCPRuntimeServers(input mcpRuntimeInput, configured map[string]json.RawMessage, cfg config.TunnelConfig) (map[string]mcpRuntimeServer, error) {
	servers := make(map[string]mcpRuntimeServer)
	if len(configured) > 0 || len(input.MCPServers) == 0 {
		// Explicit Code Session MCP documents are also a supported input boundary.
		for name, raw := range configured {
			server, err := parseMCPRuntimeServer(name, raw, nil, cfg)
			if err != nil {
				return nil, err
			}
			servers[name] = server
		}
		return servers, nil
	}
	var declarations []json.RawMessage
	if err := json.Unmarshal(input.MCPServers, &declarations); err != nil {
		return nil, err
	}
	toolsets := mcpRuntimeToolsets(input.Tools)
	for _, raw := range declarations {
		var declaration mcpServerDeclaration
		if err := json.Unmarshal(raw, &declaration); err != nil {
			return nil, err
		}
		if declaration.URL == "" {
			return nil, ErrMCPDeclarationInvalid
		}
		if _, duplicate := servers[declaration.Name]; duplicate {
			return nil, ErrMCPDeclarationInvalid
		}
		fields := make(map[string]json.RawMessage)
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		delete(fields, "name")
		if tools := toolsets[declaration.Name]; len(tools) > 0 {
			fields["tools"], _ = json.Marshal(tools)
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		server, err := parseMCPRuntimeServer(declaration.Name, encoded, &declaration, cfg)
		if err != nil {
			return nil, err
		}
		servers[declaration.Name] = server
	}
	return servers, nil
}

func parseMCPRuntimeServer(name string, raw json.RawMessage, declaration *mcpServerDeclaration, cfg config.TunnelConfig) (mcpRuntimeServer, error) {
	if !canonicalMCPServerName(name) {
		return nil, ErrMCPDeclarationInvalid
	}
	var target mcpServerDeclaration
	if err := json.Unmarshal(raw, &target); err != nil {
		return nil, err
	}
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, ErrMCPDeclarationInvalid
	}
	parsed, err := url.Parse(strings.TrimSpace(target.URL))
	if err != nil {
		return nil, fmt.Errorf("parse MCP target: %w", err)
	}
	_, recognized, err := tunnels.RecognizeTarget(parsed, cfg)
	if err != nil {
		return nil, err
	}
	if recognized {
		return tunnelMCPServer{name: name, fields: fields}, nil
	}
	return remoteMCPServer{document: raw, fields: fields, declaration: declaration}, nil
}

func (s remoteMCPServer) build(_ MCPRuntimeIdentity) (json.RawMessage, error) {
	if s.declaration == nil {
		return s.document, nil
	}
	s.fields["type"], _ = json.Marshal(remoteMCPTransportType(s.declaration.Type, s.declaration.URL))
	return json.Marshal(s.fields)
}

func (s tunnelMCPServer) build(identity MCPRuntimeIdentity) (json.RawMessage, error) {
	if identity.APIBaseURL == "" {
		return nil, ErrMCPGatewayMissing
	}
	if identity.CodeSessionID == "" || identity.SessionIngressToken == "" {
		return nil, ErrMCPRuntimeIdentityMissing
	}
	// Connection fields are wholly owned by the Tunnel builder. Preserve only
	// non-connection options (tools and opaque extensions) from the input boundary.
	fields := make(map[string]json.RawMessage, len(s.fields))
	for key, value := range s.fields {
		switch key {
		case "type", "url", "headers":
		default:
			fields[key] = value
		}
	}
	target := mcpClientTarget{
		Type:    "http",
		URL:     strings.TrimRight(identity.APIBaseURL, "/") + "/v2/ccr-sessions/" + url.PathEscape(identity.CodeSessionID) + "/mcp/" + url.PathEscape(s.name),
		Headers: map[string]string{"Authorization": "Bearer " + identity.SessionIngressToken},
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func remoteMCPTransportType(serverType, rawURL string) string {
	switch strings.ToLower(serverType) {
	case "sse":
		return "sse"
	case "http", "ws":
		return strings.ToLower(serverType)
	case "websocket":
		return "ws"
	}
	parsed, err := url.Parse(rawURL)
	if err == nil && strings.HasSuffix(strings.TrimRight(strings.ToLower(parsed.Path), "/"), "/sse") {
		return "sse"
	}
	return "http"
}

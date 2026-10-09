package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"charm.land/fantasy"
	crushruntime "github.com/charmbracelet/crush/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

type mcpConnection struct {
	session       *mcp.ClientSession
	transport     *http.Transport
	tools         []*mcp.Tool
	remoteServers map[string]bool
}

type headerTransport struct {
	next    http.RoundTripper
	headers map[string]string
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copyRequest := request.Clone(request.Context())
	for key, value := range t.headers {
		copyRequest.Header.Set(key, value)
	}
	return t.next.RoundTrip(copyRequest)
}

func connectMCP(ctx context.Context, endpoint string, headers map[string]string) (*mcpConnection, error) {
	location, err := url.Parse(endpoint)
	if err != nil || location.Host == "" || location.User != nil || location.Fragment != "" || location.Scheme != "https" && location.Scheme != "http" {
		return nil, ErrInvalidConfig
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Transport: headerTransport{next: transport, headers: headers}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	protocolClient := mcp.NewClient(&mcp.Implementation{Name: "oma-host-agent", Version: "1"}, nil)
	session, err := protocolClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, ErrMCPUnavailable
	}
	connection := &mcpConnection{session: session, transport: transport}
	if err := connection.discover(ctx); err != nil {
		connection.close()
		return nil, err
	}
	return connection, nil
}

func (c *mcpConnection) discover(ctx context.Context) error {
	cursor := ""
	seen := make(map[string]bool)
	for page := 0; page < 20; page++ {
		result, err := c.session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return ErrInvalidCatalog
		}
		for _, tool := range result.Tools {
			if tool == nil || tool.Name == "" || seen[tool.Name] || len(c.tools) >= 512 {
				return ErrInvalidCatalog
			}
			seen[tool.Name] = true
			c.tools = append(c.tools, tool)
		}
		cursor = result.NextCursor
		if cursor == "" {
			return nil
		}
	}
	return ErrInvalidCatalog
}

func (c *mcpConnection) close() {
	_ = c.session.Close()
	c.transport.CloseIdleConnections()
}

func sandboxToolName(name string) string {
	switch name {
	case "file_write", "write":
		return "Write"
	case "file_edit", "edit":
		return "Edit"
	case "grep_run", "grep":
		return "Grep"
	case "glob_run", "glob":
		return "Glob"
	case "bash", "run_in_terminal":
		return "Bash"
	case "read", "file_read":
		return "Read"
	default:
		return ""
	}
}

type mcpTool struct {
	connection *mcpConnection
	remoteName string
	info       fantasy.ToolInfo
	options    fantasy.ProviderOptions
	turn       *turn
	schema     json.RawMessage
}

func (c *mcpConnection) agentTools(current *turn) ([]fantasy.AgentTool, error) {
	var tools []fantasy.AgentTool
	seen := make(map[string]bool)
	for _, definition := range c.tools {
		name := sandboxToolName(definition.Name)
		if name == "" && c.isRemoteTool(definition.Name) {
			name = definition.Name
		}
		if name == "" {
			return nil, ErrInvalidCatalog
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		encoded, err := json.Marshal(definition.InputSchema)
		if err != nil {
			return nil, ErrInvalidCatalog
		}
		var schema struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(encoded, &schema); err != nil || schema.Type != "object" {
			return nil, ErrInvalidCatalog
		}
		tools = append(tools, &mcpTool{connection: c, remoteName: definition.Name, turn: current, schema: encoded, info: fantasy.ToolInfo{Name: name, Description: definition.Description, Parameters: schema.Properties, Required: schema.Required}})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Info().Name < tools[j].Info().Name })
	return tools, nil
}

func (t *mcpTool) Info() fantasy.ToolInfo                             { return t.info }
func (t *mcpTool) InputSchema() json.RawMessage                       { return t.schema }
func (t *mcpTool) ProviderOptions() fantasy.ProviderOptions           { return t.options }
func (t *mcpTool) SetProviderOptions(options fantasy.ProviderOptions) { t.options = options }

func (t *mcpTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	input, allowed, err := t.turn.authorize(ctx, call, t.connection.isRemoteTool(t.remoteName))
	if err != nil {
		return fantasy.ToolResponse{}, &crushruntime.ExecutionError{Cause: err}
	}
	if !allowed {
		return fantasy.NewTextErrorResponse("Tool execution was denied."), nil
	}
	marker := t.turn.history.marker("tool_dispatch", "tool-dispatch:"+call.ID)
	if err := t.turn.execution.worker.AppendHistory(ctx, []codesessions.HostHistoryEntry{marker}); err != nil {
		return fantasy.ToolResponse{}, &crushruntime.ExecutionError{Cause: err}
	}
	callCtx, cancel := context.WithTimeout(ctx, 11*time.Minute)
	defer cancel()
	result, err := t.connection.session.CallTool(callCtx, &mcp.CallToolParams{Name: t.remoteName, Arguments: input})
	if err != nil {
		return fantasy.ToolResponse{}, &crushruntime.ExecutionError{Cause: ErrMCPOutcomeUnknown}
	}
	if !result.IsError && len(result.Content) == 1 {
		if image, ok := result.Content[0].(*mcp.ImageContent); ok {
			return fantasy.NewImageResponse(image.Data, image.MIMEType), nil
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return fantasy.ToolResponse{}, &crushruntime.ExecutionError{Cause: ErrMCPOutcomeUnknown}
	}
	if result.IsError {
		return fantasy.NewTextErrorResponse(string(payload)), nil
	}
	return fantasy.NewTextResponse(string(payload)), nil
}

func gatewayServers(configuration launchConfig) (map[string]bool, error) {
	if len(configuration.MCPConfig.Servers) > 32 {
		return nil, ErrInvalidConfig
	}
	servers := make(map[string]bool)
	for name, target := range configuration.MCPConfig.Servers {
		location, err := url.Parse(target.URL)
		if name == "" || strings.Contains(name, "__") || target.Type != "http" || err != nil || location.Host == "" || location.User != nil || location.Fragment != "" || location.Scheme != "https" && location.Scheme != "http" {
			return nil, ErrInvalidConfig
		}
		wireName := strings.ReplaceAll(name, ".", "_")
		if servers[wireName] {
			return nil, ErrInvalidConfig
		}
		servers[wireName] = true
	}
	return servers, nil
}

func (c *mcpConnection) isRemoteTool(name string) bool {
	if !strings.HasPrefix(name, "mcp__") {
		return false
	}
	matches := 0
	for server := range c.remoteServers {
		prefix := "mcp__" + server + "__"
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			matches++
		}
	}
	return matches == 1
}

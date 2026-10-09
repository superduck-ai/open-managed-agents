package agentruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGatewayRejectsUndeclaredRemoteTool(t *testing.T) {
	connection := &mcpConnection{remoteServers: map[string]bool{"allowed": true}, tools: []*mcp.Tool{{Name: "mcp__unknown__call", InputSchema: map[string]any{"type": "object"}}}}
	if _, err := connection.agentTools(nil); err == nil {
		t.Fatal("undeclared server tool accepted")
	}
}

func TestGatewayRejectsNormalizedServerCollision(t *testing.T) {
	configuration := launchConfig{}
	configuration.MCPConfig.Servers = map[string]remoteTarget{
		"docs.api": {Type: "http", URL: "https://example.com/mcp"},
		"docs_api": {Type: "http", URL: "https://example.com/mcp"},
	}
	if _, err := gatewayServers(configuration); err != ErrInvalidConfig {
		t.Fatalf("normalized collision accepted: %v", err)
	}
}

func TestGatewayAcceptsNormalizedServerNames(t *testing.T) {
	configuration := launchConfig{}
	configuration.MCPConfig.Servers = map[string]remoteTarget{
		"docs.api":       {Type: "http", URL: "https://example.com/mcp"},
		"docs..internal": {Type: "http", URL: "https://example.com/mcp"},
	}
	servers, err := gatewayServers(configuration)
	if err != nil {
		t.Fatal(err)
	}
	connection := &mcpConnection{remoteServers: servers}
	for _, name := range []string{"mcp__docs_api__read", "mcp__docs__internal__read"} {
		if !connection.isRemoteTool(name) {
			t.Fatalf("normalized name rejected: %s", name)
		}
	}
	for _, name := range []string{"mcp__docs.api__read", "mcp__docs_api__", "mcp__unknown__read"} {
		if connection.isRemoteTool(name) {
			t.Fatalf("undeclared or empty name accepted: %s", name)
		}
	}
	connection.remoteServers["docs"] = true
	if connection.isRemoteTool("mcp__docs__internal__read") {
		t.Fatal("ambiguous normalized prefix accepted")
	}
}

func TestHostConnectUsesOnlySandboxGateway(t *testing.T) {
	var directRequests atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { directRequests.Add(1) }))
	defer remote.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "mcp__remote__call"}, func(context.Context, *mcp.CallToolRequest, hostSandboxInput) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})
	gateway := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer gateway.Close()
	configuration := launchConfig{Model: "test-model"}
	configuration.MCPConfig.Servers = map[string]remoteTarget{"remote": {Type: "http", URL: remote.URL}}
	executor := &execution{service: &Service{modelBaseURL: "http://127.0.0.1:1"}, configuration: configuration}
	if err := executor.connect(t.Context(), StartInput{MCPEndpoint: gateway.URL, MCPToken: "test", OAuthAccessToken: "test"}); err != nil {
		t.Fatal(err)
	}
	defer executor.closeConnections()
	tools, err := executor.connections[0].agentTools(nil)
	if err != nil || len(tools) != 1 || tools[0].Info().Name != "mcp__remote__call" {
		t.Fatalf("gateway catalog not accepted: tools=%v error=%v", tools, err)
	}
	if _, err := executor.connections[0].session.CallTool(t.Context(), &mcp.CallToolParams{Name: "mcp__remote__call", Arguments: map[string]string{"path": "test", "content": "test"}}); err != nil {
		t.Fatalf("connection ended with handshake context: %v", err)
	}
	if directRequests.Load() != 0 || len(executor.connections) != 1 {
		t.Fatalf("host bypassed sandbox: requests=%d connections=%d", directRequests.Load(), len(executor.connections))
	}
}

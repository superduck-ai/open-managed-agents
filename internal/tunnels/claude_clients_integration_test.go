package tunnels

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Real model calls are opt-in. This covers the actual clients and R3 Broker;
// the HTTP fixture deliberately does not claim full DB/Gateway/Sandbox E2E.
func TestClaudeClientsTunnelIntegration(t *testing.T) {
	for _, name := range []string{"TEST_CLAUDE_AUTH_FILE", "TEST_CLAUDE_MODEL", "TEST_CLAUDE_PYTHON", "TEST_CLAUDE_NODE_DIR", "TEST_CLAUDE_CLI", "TEST_TUNNEL_CLIENT_BINARY"} {
		if os.Getenv(name) == "" {
			t.Skip(name + " is not configured")
		}
	}
	for _, transport := range []string{"http-json", "http-sse", "stdio"} {
		t.Run(transport, func(t *testing.T) { runClaudeTransportCase(t, transport) })
	}
}

func runClaudeTransportCase(t *testing.T, transport string) {
	t.Helper()
	servers := startTunnelNATSCluster(t)
	urls := make([]string, 0, len(servers))
	for _, server := range servers {
		urls = append(urls, server.ClientURL())
	}
	broker, err := NewBroker(t.Context(), connectTunnelNATS(t, strings.Join(urls, ",")), brokerTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	endpoint, controlURL, tunnelID := tunnelHTTPFixture(t, broker)
	marker := fmt.Sprintf("claude-proof-%d", time.Now().UnixNano())
	args := []string{"run", "--profile-dir", t.TempDir(), "--control-plane.base-url", controlURL,
		"--control-plane.url-path", "/connector", "--control-plane.tunnel-id", tunnelID,
		"--control-plane.api-key", "env:OMA_TEST_CONNECTOR_TOKEN", "--control-plane.poll-timeout", "1s",
		"--health.listen-addr", "127.0.0.1:0", "--log.level", "warn"}
	if transport == "stdio" {
		args = append(args, "--mcp.command", "command="+os.Args[0]+" -test.run=^TestTunnelStdioFixture$")
	} else {
		privateMCP := newIntegrationMCP(marker)
		privateHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return privateMCP }, &mcp.StreamableHTTPOptions{JSONResponse: transport == "http-json"}))
		t.Cleanup(privateHTTP.Close)
		args = append(args, "--mcp.server-url", "url="+privateHTTP.URL)
	}
	startOfficialConnector(t, os.Getenv("TEST_TUNNEL_CLIENT_BINARY"), args, marker)
	deadline := time.Now().Add(15 * time.Second)
	for {
		snapshot, err := broker.ConnectorSnapshot(t.Context(), "tunnel")
		if err == nil && snapshot.InstanceCount > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connector did not register")
		}
		time.Sleep(25 * time.Millisecond)
	}
	script, err := filepath.Abs("../../tests/e2e/claude/tunnel_clients.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"typescript", "python", "cli"} {
		t.Run(client, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, os.Getenv("TEST_CLAUDE_PYTHON"), script, client)
			command.Dir = t.TempDir()
			command.Env = append(os.Environ(), "TEST_TUNNEL_ENDPOINT="+endpoint, "TEST_EXPECTED_MARKER="+marker)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("%s client: %v\n%s", client, err, output)
			}
			t.Log(strings.TrimSpace(string(output)))
		})
	}
}

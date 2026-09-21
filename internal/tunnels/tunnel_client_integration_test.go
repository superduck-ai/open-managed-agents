package tunnels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// This exercises real HTTP handlers, NATS, the unmodified connector binary and
// an MCP SDK transport. DB authorization is a fixture; Claude SDK/CLI and the
// full Managed Agent runtime remain separate acceptance gates.
func TestOfficialTunnelClientIntegration(t *testing.T) {
	binary := os.Getenv("TEST_TUNNEL_CLIENT_BINARY")
	if binary == "" {
		t.Skip("TEST_TUNNEL_CLIENT_BINARY is not configured")
	}
	for _, transport := range []string{"http-json", "http-sse", "stdio", "http-v2"} {
		t.Run(transport, func(t *testing.T) { runOfficialTunnelClientCase(t, binary, transport) })
	}
}

func runOfficialTunnelClientCase(t *testing.T, binary, transport string) {
	t.Helper()
	marker := fmt.Sprintf("tunnel-proof-%d", time.Now().UnixNano())
	b := testNATSBroker(t, brokerTestConfig())
	if transport == "stdio" {
		b.cfg.PresenceTTL = time.Second
	}
	endpoint, controlURL, tunnelID := tunnelHTTPFixture(t, b)
	args := []string{"run", "--profile-dir", t.TempDir(), "--control-plane.base-url", controlURL, "--control-plane.url-path", "/connector",
		"--control-plane.tunnel-id", tunnelID, "--control-plane.api-key", "env:OMA_TEST_CONNECTOR_TOKEN",
		"--control-plane.poll-timeout", "1s", "--health.listen-addr", "127.0.0.1:0", "--log.level", "warn"}
	if transport == "stdio" {
		args = append(args, "--mcp.command", "command="+os.Args[0]+" -test.run=^TestTunnelStdioFixture$")
	} else {
		privateMCP := newIntegrationMCP(marker)
		privateHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return privateMCP }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: transport == "http-json"}))
		t.Cleanup(privateHTTP.Close)
		args = append(args, "--mcp.server-url", "url="+privateHTTP.URL)
		if transport == "http-v2" {
			args = append(args, "--harpoon.allow-plaintext-http", "--harpoon.target", "label=proof,url="+privateHTTP.URL)
		}
	}
	startOfficialConnector(t, binary, args, marker)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "oma-tunnel-proof", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("initialize through official connector: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "tunnel_proof" {
		t.Fatalf("tools/list = %+v, %v", tools, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "tunnel_proof", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), marker) {
		t.Fatalf("missing per-run tool marker: %s, %v", encoded, err)
	}
	closed := make(chan error, 1)
	go func() { closed <- session.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close MCP session: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("session close stalled; malformed DELETE must not be queued")
	}
}

func tunnelHTTPFixture(t *testing.T, b *Broker) (string, string, string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	credential := activeConnectorContext()
	credential.TunnelUUID = "tunnel"
	tunnel := db.MCPTunnel{UUID: "tunnel", ExternalID: credential.TunnelExternalID}
	connector := NewConnectorHandler(b.cfg, &db.DB{}, b, nil, logger)
	connector.db = connectorMetadataDatabase{context: credential, tunnel: tunnel, expectedToken: "valid-token"}
	ingress := NewIngressHandler(b.cfg, &db.DB{}, b, logger)
	router := chi.NewRouter()
	router.Mount("/connector", connector)
	router.Get("/mcp", ingress.getSSENotSupported)
	forward := func(w http.ResponseWriter, r *http.Request) error {
		kind := CommandTypeJSONRPC
		if r.Method == http.MethodDelete {
			kind = CommandTypeSessionTermination
		}
		return ingress.forwardTunnel(w, r, tunnel, "main", kind)
	}
	router.Post("/mcp", ingress.errorAdapter.Wrap(forward))
	router.Delete("/mcp", ingress.errorAdapter.Wrap(forward))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server.URL + "/mcp", server.URL, tunnel.ExternalID
}

type integrationToolOutput struct {
	Marker string `json:"marker"`
}

func newIntegrationMCP(marker string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "oma-private-proof", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "tunnel_proof", Description: "Return the unique verification marker for this run."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, integrationToolOutput, error) {
			return nil, integrationToolOutput{Marker: marker}, nil
		})
	return server
}

func TestTunnelStdioFixture(t *testing.T) {
	if os.Getenv("OMA_TEST_STDIO_FIXTURE") != "1" {
		t.Skip("subprocess fixture")
	}
	if err := newIntegrationMCP(os.Getenv("OMA_TEST_TOOL_MARKER")).Run(t.Context(), &mcp.StdioTransport{}); err != nil {
		t.Fatal(err)
	}
}

func startOfficialConnector(t *testing.T, binary string, args []string, marker string) func() {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "connector.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, args...)
	command.Env = append(os.Environ(), "OMA_TEST_CONNECTOR_TOKEN=valid-token", "OMA_TEST_STDIO_FIXTURE=1", "OMA_TEST_TOOL_MARKER="+marker)
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	stop := sync.OnceFunc(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-finished
		}
		_ = log.Close()
		if t.Failed() {
			if data, err := os.ReadFile(logPath); err == nil {
				if len(data) > 16000 {
					data = data[len(data)-16000:]
				}
				t.Logf("connector log: %s", data)
			}
		}
	})
	t.Cleanup(stop)
	return stop
}

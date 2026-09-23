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
		privateHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return privateMCP }, &mcp.StreamableHTTPOptions{JSONResponse: transport == "http-json"}))
		t.Cleanup(privateHTTP.Close)
		args = append(args, "--mcp.server-url", "url="+privateHTTP.URL)
		if transport == "http-v2" {
			args = append(args, "--harpoon.allow-plaintext-http", "--harpoon.target", "label=proof,url="+privateHTTP.URL)
		}
	}
	stopConnector := startOfficialConnector(t, binary, args, marker)
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		snapshot, err := b.ConnectorSnapshot(t.Context(), "tunnel")
		if err == nil && snapshot.InstanceCount > 0 {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("official tunnel-client did not register")
	}
	if transport == "http-v2" {
		var control tunnelControl
		if _, err := b.control.read(t.Context(), brokerKey("tunnel"), &control); err != nil {
			t.Fatal(err)
		}
		harpoon := control.Channels["harpoon"]
		if harpoon == nil || !harpoon.Declaration.Stateless || !harpoon.Declaration.ProcessAffinity {
			t.Fatal("official v2 declaration lost independent capabilities")
		}
	}
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
	if transport == "stdio" {
		stopConnector()
		time.Sleep(b.cfg.PresenceTTL + 100*time.Millisecond)
		startOfficialConnector(t, binary, args, marker)
		verifyOfficialConnectorRestart(t, b, endpoint, session.ID(), marker)
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
	credential.Token.TunnelUUID = "tunnel"
	tunnel := db.MCPTunnel{UUID: "tunnel", ExternalID: credential.TunnelExternalID}
	connector := NewConnectorHandler(b.cfg, &db.DB{}, b, logger)
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

func verifyOfficialConnectorRestart(t *testing.T, b *Broker, endpoint, oldID, marker string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		snapshot, err := b.ConnectorSnapshot(ctx, "tunnel")
		if err == nil && snapshot.InstanceCount > 0 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("restarted connector did not register")
		}
		time.Sleep(25 * time.Millisecond)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Mcp-Session-Id", oldID)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("old session after restart = %d, want 404", response.StatusCode)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "restart-proof", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("reinitialize after process restart: %v", err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "tunnel_proof", Arguments: map[string]any{}})
	encoded, encodeErr := json.Marshal(result)
	if err != nil || encodeErr != nil || !strings.Contains(string(encoded), marker) {
		t.Fatalf("tool after restart = %s, %v, %v", encoded, err, encodeErr)
	}
}

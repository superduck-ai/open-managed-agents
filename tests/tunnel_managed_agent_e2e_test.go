//go:build e2e

package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	e2b "github.com/superduck-ai/e2b-go-sdk"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

// This deliberately owns a separate database: newTestApp replaces test LLM
// providers, so pointing it at a developer's existing database is unsafe.
func TestManagedAgentNATSTunnelE2E(t *testing.T) {
	// A channel name is not a transport hint: exercise the full launch path with
	// the name that would otherwise be mistaken for legacy SSE.
	const channel = "sse"
	if os.Getenv("TEST_MANAGED_TUNNEL_E2E") != "1" {
		t.Skip("real sandbox and model calls require TEST_MANAGED_TUNNEL_E2E=1")
	}
	for _, name := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "TEST_CLAUDE_MODEL", "TEST_TUNNEL_CLIENT_BINARY"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required", name)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !quickstartShouldRunRealSandbox(cfg) {
		t.Fatal("enabled Managed Agent gate requires real E2B credentials and debug=false")
	}
	quickstartRequireRealSandboxConfig(t, cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	cfg.Database.URL = managedTunnelDatabase(t, cfg.Database.URL)
	cfg.CodeSession.SandboxAPIBaseURL = "http://host.docker.internal:18080"
	cfg.Tunnel.PublicBaseURL = "http://127.0.0.1:18080"
	cfg.E2B.RequestTimeout = 2 * time.Minute
	cfg.E2B.SandboxTimeout = 5 * time.Minute
	app := newTestAppWithStore(t, &cfg, newFakeStore(cfg.Storage.S3.Bucket))
	t.Cleanup(app.close)
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "Isolated Claude tunnel acceptance", managedTunnelModelProxy(t), os.Getenv("ANTHROPIC_AUTH_TOKEN"), os.Getenv("TEST_CLAUDE_MODEL"))
	connection := managedTunnelNATS(t)
	broker, err := tunnels.NewBroker(ctx, connection, cfg.Tunnel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	workerBroker, err := workerevents.NewJetStream(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eventBus, err := sessionfanout.NewNATS(ctx, connection, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := eventBus.Close(); err != nil {
			t.Error(err)
		}
	})
	var gatewayCalls atomic.Int64
	provider := e2bruntime.NewProvider(cfg.E2B)
	handler := api.NewServer(api.ServerDeps{
		Config: cfg, DB: app.db, ObjectStore: app.store, Deployments: app.deployments,
		Logger:                 logger,
		CodeSessionCredentials: app.credentials, FilestoreCredentials: app.filestoreCredentials,
		VaultSecrets: app.vaultSecrets, TunnelBroker: broker, TunnelCleanupJobs: tunnels.NewCleanupJobs(app.deploymentJobs),
		WorkerEventBroker: workerBroker, SessionEventBus: eventBus,
		SandboxTimeoutExtender: provider,
	})
	app.server.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	app.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v2/ccr-sessions/") && strings.Contains(r.URL.Path, "/mcp/") {
			gatewayCalls.Add(1)
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			handler.ServeHTTP(wrapped, r)
			t.Logf("Gateway method=%s status=%d", r.Method, wrapped.Status())
			return
		}
		handler.ServeHTTP(w, r)
	}))
	_ = app.server.Listener.Close()
	app.server.Listener = listener
	app.server.Start()
	app.baseURL, app.client = app.server.URL, app.server.Client()
	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	tunnel, err := client.Beta.Tunnels.New(ctx, anthropic.BetaTunnelNewParams{DisplayName: anthropic.String("Isolated Managed Agent proof")})
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.Beta.Tunnels.RevealToken(ctx, tunnel.ID, anthropic.BetaTunnelRevealTokenParams{})
	if err != nil {
		t.Fatal(err)
	}
	marker := fmt.Sprintf("managed-tunnel-proof-%d", time.Now().UnixNano())
	var toolCalls atomic.Int64
	privateMCP := mcp.NewServer(&mcp.Implementation{Name: "private-managed-proof", Version: "1"}, nil)
	mcp.AddTool(privateMCP, &mcp.Tool{Name: "tunnel_proof", Description: "Return a unique verification marker."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct {
			Marker string `json:"marker"`
		}, error) {
			toolCalls.Add(1)
			return nil, struct {
				Marker string `json:"marker"`
			}{marker}, nil
		})
	privateHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return privateMCP }, &mcp.StreamableHTTPOptions{JSONResponse: true}))
	t.Cleanup(privateHTTP.Close)
	managedTunnelConnector(t, app.baseURL, tunnel.ID, token.TunnelToken, privateHTTP.URL, channel)
	deadline := time.Now().Add(15 * time.Second)
	for {
		result, _, probeErr := tunnels.NewService(cfg.Tunnel, app.db, app.vaultSecrets, broker, tunnels.NewCleanupJobs(app.deploymentJobs)).ProbeTarget(ctx, tunnels.ConsoleScope{
			OrganizationUUID: getDefaultDBIDs(t, app.pool).OrganizationUUID, WorkspaceUUID: getDefaultDBIDs(t, app.pool).WorkspaceUUID,
		}, cfg.Tunnel.PublicBaseURL+"/v1/mcp/"+tunnel.ID+"/"+channel)
		if probeErr == nil && len(result.Tools) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("official connector did not expose the proof tool")
		}
		time.Sleep(100 * time.Millisecond)
	}
	agent := createAgent(t, app, fmt.Sprintf(`{"model":%q,"name":"Managed tunnel proof","system":"Use the requested MCP tool and return its result.","mcp_servers":[{"type":"url","name":"tunnel","url":%q}],"tools":[{"type":"mcp_toolset","mcp_server_name":"tunnel","configs":[{"name":"tunnel_proof","enabled":true,"permission_policy":{"type":"always_allow"}}]}]}`, os.Getenv("TEST_CLAUDE_MODEL"), cfg.Tunnel.PublicBaseURL+"/v1/mcp/"+tunnel.ID+"/"+channel))
	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: marker,
		Config: anthropic.BetaEnvironmentNewParamsConfigUnion{OfCloud: &anthropic.BetaCloudConfigParams{
			Networking: anthropic.BetaCloudConfigParamsNetworkingUnion{OfUnrestricted: &anthropic.BetaUnrestrictedNetworkParam{}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent: anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)}, EnvironmentID: environment.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
			Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
			Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{OfText: &anthropic.BetaManagedAgentsTextBlockParam{
				Type: anthropic.BetaManagedAgentsTextBlockTypeText, Text: "Call mcp__tunnel__tunnel_proof exactly once and return the marker from its result verbatim. Do not guess it.",
			}}},
		}}},
	})
	if err != nil || len(events.Data) != 1 {
		t.Fatalf("send proof task: %v", err)
	}
	workID := quickstartFindSessionEnvironmentWorkID(t, app, environment.ID, session.ID)
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer stopCancel()
		quickstartStopEnvironmentWork(t, stopCtx, app, environment.ID, workID)
	})
	runner, err := environments.NewRunner(environments.RunnerDependencies{
		DB: app.db, Provider: provider, Config: cfg,
		CodeSessions: codesessions.NewServiceWithCredentials(app.db, app.credentials, logger).WithWorkerEventBroker(workerBroker),
		Skills:       skillsapi.NewRuntimeResolver(app.db), FilestoreTokens: app.filestoreCredentials,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("starting real Sandbox with isolated DB, authenticated Gateway, and R3 NATS")
	processed, err := runner.RunOnce(ctx, "managed-tunnel-e2e")
	if err != nil || !processed {
		t.Fatalf("start managed agent: processed=%t error=%v", processed, err)
	}
	sandboxID, _ := quickstartWaitForProviderSandboxMetadata(t, ctx, app, environment.ID, workID)
	sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{ConnectionOpts: e2bruntime.ConnectionOptsFromConfig(cfg.E2B)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		diagnosticCtx, diagnosticCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer diagnosticCancel()
		// Only process names, public health status, versions, and fixed error
		// categories may leave the sandbox; never dump config, env, or log lines.
		stdout, _, diagnosticErr := runE2BCommand(diagnosticCtx, sandbox, `
ps -eo comm= | sort -u
curl --noproxy '*' --max-time 5 -s -o /dev/null -w 'host_health=%{http_code}\n' http://host.docker.internal:18080/healthz
/opt/claude-code/bin/claude --version
for logfile in /tmp/claude-code-sessions/*/environment-manager.log; do
  test -f "$logfile" || continue
  wc -c < "$logfile"
  grep -Eo 'ENOENT|ECONNREFUSED|ENOTFOUND|ETIMEDOUT|Unauthorized|Forbidden|invalid token|unknown command|unrecognized arguments|not found|permission denied|worker_epoch' "$logfile" | sort | uniq -c
done
`, 20*time.Second)
		t.Logf("sandbox diagnostics: %s; error=%v; gateway requests=%d private tool calls=%d", stdout, diagnosticErr, gatewayCalls.Load(), toolCalls.Load())
	})
	waitForManagedTunnelProof(t, ctx, app, session.ID, marker)
	if toolCalls.Load() != 1 || gatewayCalls.Load() == 0 {
		t.Fatalf("private tool calls=%d gateway requests=%d", toolCalls.Load(), gatewayCalls.Load())
	}
	t.Logf("verified real Sandbox answer, private tool calls=%d, Gateway requests=%d", toolCalls.Load(), gatewayCalls.Load())
}

func managedTunnelModelProxy(t *testing.T) string {
	t.Helper()
	target, err := url.Parse(os.Getenv("ANTHROPIC_BASE_URL"))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		t.Fatal("ANTHROPIC_BASE_URL must be an HTTP(S) URL")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	direct := proxy.Director
	proxy.Director = func(r *http.Request) { direct(r); r.Host = target.Host }
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "test model proxy upstream unavailable", http.StatusBadGateway)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages") {
			body, err := io.ReadAll(io.LimitReader(r.Body, (32<<20)+1))
			_ = r.Body.Close()
			if err != nil || len(body) > 32<<20 {
				http.Error(w, "test model request exceeds capture budget", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var metadata struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			}
			if err := json.Unmarshal(body, &metadata); err == nil {
				hasProof, exactName := false, false
				for _, tool := range metadata.Tools {
					hasProof = hasProof || strings.HasSuffix(tool.Name, "tunnel_proof")
					exactName = exactName || tool.Name == "mcp__tunnel__tunnel_proof"
				}
				t.Logf("model request tools=%d includes_tunnel_proof=%t exact_name=%t", len(metadata.Tools), hasProof, exactName)
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func waitForManagedTunnelProof(t *testing.T, ctx context.Context, app *testApp, sessionID, marker string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	for {
		page := listSessionEvents(t, app, sessionID, "order=asc&limit=1000", defaultTestKey)
		for _, raw := range page.Data {
			var event map[string]any
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			if event["type"] == "agent.message" && strings.Contains(rawAgentMessageText(event), marker) {
				return
			}
		}
		// An idle event can precede a following tool turn in the worker bridge.
		// Only the actual marker closes this gate; do not abort on intermediate idle.
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatal("Managed Agent did not return the private tool marker")
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func managedTunnelConnector(t *testing.T, controlURL, tunnelID, token, privateURL, channel string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, os.Getenv("TEST_TUNNEL_CLIENT_BINARY"), "run", "--profile-dir", t.TempDir(),
		"--control-plane.base-url", controlURL, "--control-plane.url-path", "/connector", "--control-plane.tunnel-id", tunnelID,
		"--control-plane.api-key", "env:OMA_MANAGED_TEST_TOKEN", "--control-plane.poll-timeout", "1s",
		"--mcp.server-url", "url="+privateURL,
		"--mcp.server-url", "channel="+channel+",url="+privateURL, "--health.listen-addr", "127.0.0.1:0", "--log.level", "warn")
	command.Env = append(os.Environ(), "OMA_MANAGED_TEST_TOKEN="+token)
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
}

func managedTunnelDatabase(t *testing.T, sourceURL string) string {
	t.Helper()
	target, err := url.Parse(sourceURL)
	if err != nil || (target.Hostname() != "localhost" && target.Hostname() != "127.0.0.1") {
		t.Fatal("isolated acceptance requires a local PostgreSQL server")
	}
	target.Path = "/postgres"
	maintenance, err := sql.Open("pgx", target.String())
	if err != nil {
		t.Fatal(err)
	}
	name := "oma_tunnel_e2e_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := maintenance.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		_ = maintenance.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := maintenance.ExecContext(ctx, "DROP DATABASE "+name); err != nil {
			t.Errorf("cleanup owned test database %s: %v", name, err)
		}
		_ = maintenance.Close()
	})
	target.Path = "/" + name
	return target.String()
}

func managedTunnelNATS(t *testing.T) *nats.Conn {
	t.Helper()
	var nodes []*server.Server
	var urls []string
	ports := make([]int, 3)
	for i := range ports {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ports[i] = listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 {
		var routes []*url.URL
		for j, port := range ports {
			if i != j {
				routes = append(routes, &url.URL{Scheme: "nats-route", Host: "127.0.0.1:" + strconv.Itoa(port)})
			}
		}
		node, err := server.NewServer(&server.Options{
			Host: "127.0.0.1", Port: -1, ServerName: fmt.Sprintf("managed-tunnel-%d", i),
			JetStream: true, StoreDir: t.TempDir(), MaxPayload: 2 << 20, NoLog: true, NoSigs: true,
			Cluster: server.ClusterOpts{Name: "managed-tunnel", Host: "127.0.0.1", Port: ports[i]}, Routes: routes,
		})
		if err != nil {
			t.Fatal(err)
		}
		go node.Start()
		t.Cleanup(func() { node.Shutdown(); node.WaitForShutdown() })
		if !node.ReadyForConnections(5 * time.Second) {
			t.Fatal("NATS node did not start")
		}
		urls = append(urls, node.ClientURL())
		nodes = append(nodes, node)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ready := false
		for _, node := range nodes {
			ready = ready || (node.JetStreamIsLeader() && len(node.JetStreamClusterPeers()) == 3)
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("NATS R3 cluster did not converge")
		}
		time.Sleep(25 * time.Millisecond)
	}
	connection, err := nats.Connect(strings.Join(urls, ","), nats.ReconnectBufSize(-1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	return connection
}

package liveworker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/nats-io/nats.go"
	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

func TestChatHostStart(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version":  e.agent.CurrentVersion,
		"model":    map[string]string{"id": "claude-sonnet-4-6"},
		"metadata": map[string]string{"agent_runtime_mode": "host"},
		"tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{
			"enabled": true, "permission_policy": map[string]string{"type": "always_ask"},
		}}},
	}, 200)
	provider, stop := startHostPublicRunner(t, e)
	defer stop()
	client := chatSDK(e)
	for _, decision := range []anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResult{"deny", "allow"} {
		passed := t.Run(string(decision), func(t *testing.T) {
			verifyHostPublicTurn(t, e, &client, decision)
		})
		if !passed {
			continue
		}
		stage := "host_tool_denied"
		if decision == "allow" {
			stage = "host_tool_allowed"
		}
		chatProof(t, started, stage)
	}
	if t.Failed() {
		return
	}
	if provider.created.Load() != 2 {
		t.Fatalf("sandbox allocations=%d, want two", provider.created.Load())
	}
	chatProof(t, started, "host_public_started")
	chatProof(t, started, "host_history_continued")
	chatProof(t, started, "host_sandbox_processes")
}

func verifyHostPublicTurn(t *testing.T, e *liveEnv, client *anthropic.Client, decision anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResult) {
	t.Helper()
	var calls, queued atomic.Int32
	resume := make(chan struct{})
	close(resume)
	modelURL := realWorkerModelFixture(t, &calls, &queued, false, resume, "/tmp/oma-control-e2e.txt")
	configureChatModel(t, e, strings.Replace(modelURL, "host.docker.internal", "127.0.0.1", 1))
	f := createPublicChat(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	submitChat(t, f, "Write the verification file using the requested tool, then reply done.")
	toolID := waitSDKToolPermission(t, client, f.session.ExternalID)
	var err error
	f.code, err = e.database.GetCodeSessionBySessionExternalID(ctx, e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	mode, err := agentruntime.PersistedMode(f.code.Metadata)
	requireOK(t, err)
	if mode != agentruntime.Host {
		t.Fatal("Runner did not persist host mode")
	}
	sandbox, err := e.database.GetResumableEnvironmentSandboxForCodeSession(ctx, f.code.ExternalID)
	requireOK(t, err)
	if sandbox.ProviderSandboxID == nil {
		t.Fatal("Runner did not bind a sandbox")
	}
	registerPublicSandboxCleanup(t, e, f)
	worker := &realControlWorker{container: *sandbox.ProviderSandboxID}
	assertToolFile(t, worker, false)
	sendChatSDK(t, client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{
		OfUserToolConfirmation: &anthropic.BetaManagedAgentsUserToolConfirmationEventParams{Type: "user.tool_confirmation", ToolUseID: toolID, Result: decision},
	})
	receiveHostAnswer(t, ctx, events)
	waitChatIdle(t, f, 1)
	assertToolFile(t, worker, decision == "allow")
	assertSDKToolHistory(t, chatSDKHistory(t, client, f.session.ExternalID), toolID, decision, 1)
	if calls.Load() != 2 {
		t.Fatalf("model calls=%d, want two", calls.Load())
	}
	submitChat(t, f, "Reply queued done after the previous task.")
	receiveHostAnswer(t, ctx, events)
	waitChatIdle(t, f, 2)
	if calls.Load() != 3 || queued.Load() != 3 {
		t.Fatalf("continued model calls=%d queued=%d", calls.Load(), queued.Load())
	}
	assertHostHistory(t, f)
	processes, err := exec.CommandContext(ctx, "docker", "top", worker.container, "-eo", "pid,args").CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox process inspection: %v: %s", err, processes)
	}
	if !strings.Contains(string(processes), "mcp-server -transport=http") || strings.Contains(string(processes), "/opt/claude-code") || strings.Contains(string(processes), "bun ") {
		t.Fatalf("unexpected sandbox processes: %s", processes)
	}
}

func receiveHostAnswer(t *testing.T, ctx context.Context, events <-chan chatEvent) {
	t.Helper()
	var previewID, text string
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for host answer")
		case event, open := <-events:
			if !open {
				t.Fatal("SSE closed before host answer")
			}
			switch event.Type {
			case "event_start":
				previewID, text = event.Event.ID, ""
			case "event_delta":
				if event.EventID == previewID {
					text += event.Delta.Content.Text
				}
			case "agent.message":
				if event.ID != previewID || text != "done" || len(event.Content) != 1 || event.Content[0].Text != text {
					t.Fatalf("host preview/final mismatch: preview=%s text=%q final=%+v", previewID, text, event)
				}
				return
			}
		}
	}
}

func assertHostHistory(t *testing.T, f *liveSession) {
	t.Helper()
	entries, more, err := f.env.service.EventPayloadStore().ListCodeSessionInternalEventsPage(t.Context(), db.ListCodeSessionInternalEventsPageParams{
		WorkspaceUUID: f.code.WorkspaceUUID, CodeSessionExternalID: f.code.ExternalID, Limit: 100,
	})
	requireOK(t, err)
	if more {
		t.Fatal("unexpected history pagination")
	}
	var users, results int
	for _, entry := range entries {
		var envelope struct {
			Entry codesessions.HostHistoryEntry `json:"host_entry"`
		}
		requireOK(t, json.Unmarshal(entry.Payload, &envelope))
		if envelope.Entry.Role != "message" {
			continue
		}
		var message fantasy.Message
		requireOK(t, json.Unmarshal(envelope.Entry.Payload, &message))
		if message.Role == fantasy.MessageRoleUser {
			users++
		}
		if message.Role == fantasy.MessageRoleTool {
			results++
		}
	}
	if users != 2 || results != 1 {
		t.Fatalf("private history users=%d results=%d", users, results)
	}
}

func startHostPublicRunner(t *testing.T, e *liveEnv) (*chatDockerProvider, func()) {
	t.Helper()
	cfg, err := config.Load()
	requireOK(t, err)
	target, err := url.Parse(e.url)
	requireOK(t, err)
	cfg.CodeSession.SandboxAPIBaseURL = serveRealWorkerFixture(t, httputil.NewSingleHostReverseProxy(target))
	cfg.CodeSession.UpstreamProxyMITMEnabled = false
	cfg.EnvironmentRunner.Enabled, cfg.EnvironmentRunner.Concurrency = true, 2
	cfg.EnvironmentRunner.SandboxMCPPort = 3001
	cfg.EnvironmentRunner.SandboxMCPCommand = `mkdir -p "$OMA_SANDBOX_WORK_DIR"; exec /usr/local/bin/mcp-server -transport=http -addr=":$OMA_SANDBOX_MCP_PORT" -workdir="$OMA_SANDBOX_WORK_DIR"`
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	connection, err := nats.Connect(cfg.NATS.URL)
	requireOK(t, err)
	t.Cleanup(connection.Close)
	bus, err := sessionfanout.NewNATS(t.Context(), connection, logger)
	requireOK(t, err)
	t.Cleanup(func() { _ = bus.Close() })
	sessionsapi.NewHandler(cfg, e.database, e.service, nil, bus, nil, logger)
	host, err := agentruntime.New(e.service, target.Host, logger)
	requireOK(t, err)
	credentials, err := filestore.NewTokenCredentials(cfg)
	requireOK(t, err)
	provider := &chatDockerProvider{name: "oma-public-" + os.Getenv("VERIFY_BE_RUN_ID"), image: os.Getenv("OMA_WORKER_CONTROL_IMAGE"), t: t, mcpPort: 3001}
	runner, err := environments.NewRunner(environments.RunnerDependencies{DB: e.database, Provider: provider, Config: cfg, CodeSessions: e.service, HostAgents: host, Skills: skillsapi.NewRuntimeResolver(e.database), FilestoreTokens: credentials, Logger: logger})
	requireOK(t, err)
	stopRunner := runner.Start(t.Context())
	return provider, func() {
		stopRunner()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		requireOK(t, host.Close(ctx))
	}
}

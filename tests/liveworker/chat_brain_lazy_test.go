//go:build integration || e2e || e2b_integration

package liveworker

import (
	"context"
	"fmt"
	"log/slog"
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

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

func (p *chatDockerProvider) ServiceEndpoint(ctx context.Context, sandbox string, port int, path string) (string, error) {
	output, err := exec.CommandContext(ctx, "docker", "port", sandbox, strconv.Itoa(port)+"/tcp").Output()
	if err != nil {
		return "", err
	}
	return "http://" + strings.TrimSpace(string(output)) + path, nil
}

func verifyLazyBrain(t *testing.T, started time.Time) {
	t.Helper()
	e := newLiveEnv(t)
	var sessions []string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, session := range sessions {
			work, err := e.database.GetLatestEnvironmentWorkForSession(ctx, e.key.WorkspaceUUID.String(), e.environment.ExternalID, session)
			if err != nil || work.State != "stopped" {
				t.Errorf("deleted Brain work remains active: state=%s error=%v", work.State, err)
			}
		}
		chatProof(t, started, "brain_lazy_cleanup")
	})
	cfg, err := config.Load()
	requireOK(t, err)
	provider, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	first := startBrainNode(t, e, cfg, "lazy-first")
	defer first.stop(t)
	second := startBrainNode(t, e, cfg, "lazy-second")
	defer second.stop(t)
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion,
		"tools":   []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": false, "permission_policy": map[string]string{"type": "always_allow"}}}},
	}, 200)
	e.agent, err = e.database.GetAgent(t.Context(), e.key.WorkspaceUUID.String(), e.agent.ExternalID)
	requireOK(t, err)
	denied := createPublicChat(t, e)
	sessions = append(sessions, denied.session.UUID)
	deniedModel := brainToolModel(t)
	configureChatModel(t, e, deniedModel.URL)
	submitChat(t, denied, "Try Write, then reply done even if denied.")
	waitRealWorker(t, "denied tool finished without allocating a sandbox", func() bool {
		session, _, err := e.database.GetSession(t.Context(), e.key.WorkspaceUUID.String(), denied.session.ExternalID)
		requireOK(t, err)
		return toolModelCalls.Load() == 2 && session.Status == "idle"
	})
	if provider.created.Load() != 0 {
		t.Fatal("denied tool created a sandbox")
	}
	chatProof(t, started, "brain_lazy_denied")
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion,
		"tools":   []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}},
	}, 200)
	preview := make(chan struct{}, 4)
	var calls atomic.Int32
	model := chatModelFixture(t, preview, &calls, false)
	configureChatModel(t, e, model.URL)
	f := createPublicChat(t, e)
	sessions = append(sessions, f.session.UUID)
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	input := submitChat(t, f, "请直接回复固定文本。")
	final := receiveChatAnswer(t, ctx, events, func() { preview <- struct{}{} })
	f.code, err = e.database.GetCodeSessionBySessionExternalID(ctx, e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	verifyChatHistory(t, f, input, final)
	waitChatIdle(t, f, 1)
	if provider.created.Load() != 0 {
		t.Fatal("text-only public Brain session allocated a sandbox")
	}
	chatProof(t, started, "brain_lazy_text")
	toolModel := brainToolModel(t)
	configureChatModel(t, e, toolModel.URL)
	submitChat(t, f, "Use Write to write /tmp/oma-brain-proof.txt, then reply done.")
	waitRealWorker(t, "lazy sandbox tool finished", func() bool {
		session, _, err := e.database.GetSession(t.Context(), f.code.WorkspaceUUID, f.session.ExternalID)
		requireOK(t, err)
		return toolModelCalls.Load() == 2 && session.Status == "idle"
	})
	if provider.created.Load() != 1 {
		t.Fatalf("sandbox allocations=%d, want one", provider.created.Load())
	}
	output, err := exec.CommandContext(ctx, "docker", "exec", fmt.Sprintf("%s-1", provider.name), "cat", "/tmp/oma-brain-proof.txt").Output()
	requireOK(t, err)
	if string(output) != "native grok loop" {
		t.Fatal("lazy tool side effect missing")
	}
	toolModel = brainToolModel(t)
	configureChatModel(t, e, toolModel.URL)
	submitChat(t, f, "Write the same proof again and reply done.")
	waitRealWorker(t, "warm sandbox reused", func() bool { return toolModelCalls.Load() == 2 })
	waitChatIdle(t, f, 3)
	if provider.created.Load() != 1 {
		t.Fatal("warm tool created a second sandbox")
	}
	registerPublicSandboxCleanup(t, e, f)
	chatProof(t, started, "brain_lazy_tool")
}

func startLazyBrainRunner(t *testing.T, e *liveEnv, cfg config.Config) (*chatDockerProvider, func()) {
	t.Helper()
	target, err := url.Parse(e.url)
	requireOK(t, err)
	cfg.CodeSession.SandboxAPIBaseURL = serveRealWorkerFixture(t, httputil.NewSingleHostReverseProxy(target))
	cfg.CodeSession.UpstreamProxyMITMEnabled = false
	cfg.EnvironmentRunner.Enabled = true
	cfg.EnvironmentRunner.AgentMode = "brain"
	cfg.EnvironmentRunner.Concurrency = 2
	credentials, err := filestore.NewTokenCredentials(cfg)
	requireOK(t, err)
	provider := &chatDockerProvider{name: "oma-public-" + os.Getenv("VERIFY_BE_RUN_ID") + "-brain", image: os.Getenv("OMA_WORKER_CONTROL_IMAGE"), t: t, publishMCP: true}
	var stops []func()
	var provisioner *environments.Runner
	for range 2 {
		runner, err := environments.NewRunner(environments.RunnerDependencies{DB: e.database, Provider: provider, Config: cfg, CodeSessions: e.service, Skills: skillsapi.NewRuntimeResolver(e.database), FilestoreTokens: credentials})
		requireOK(t, err)
		if provisioner == nil {
			provisioner = runner
		}
		stops = append(stops, runner.Start(t.Context()))
	}
	originalURL := e.url
	service := codesessions.NewServiceWithCredentials(e.database, e.credentials, slog.Default()).WithSandboxProvisioner(provisioner)
	handler := codesessions.NewHandler(cfg, service, provider, slog.Default())
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	proxy := httputil.NewSingleHostReverseProxy(target)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v2/ccr-sessions/") && strings.HasSuffix(r.URL.Path, "/sandbox/mcp") {
			router.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	e.url = gateway.URL
	return provider, func() {
		e.url = originalURL
		gateway.Close()
		for _, stop := range stops {
			stop()
		}
	}
}

func (p *chatDockerProvider) SetTimeout(context.Context, string, time.Duration) error { return nil }

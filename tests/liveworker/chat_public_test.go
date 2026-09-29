package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

type chatDockerProvider struct {
	name, image string
	t           *testing.T
	created     atomic.Int32
	failNext    atomic.Bool
}

func (p *chatDockerProvider) Resolve(env db.Environment, work *db.EnvironmentWork) (e2bruntime.Resolution, error) {
	return e2bruntime.NewProvider(config.E2BConfig{}).Resolve(env, work)
}
func (p *chatDockerProvider) Create(ctx context.Context, _ db.Environment, _ *db.EnvironmentWork, _ e2bruntime.Resolution) (e2bruntime.Sandbox, error) {
	if p.failNext.Swap(false) {
		return e2bruntime.Sandbox{}, errors.New("injected sandbox allocation failure")
	}
	name := fmt.Sprintf("%s-%d", p.name, p.created.Add(1))
	p.t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = p.Kill(cleanupCtx, name)
	})
	err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm", "--pull=never", "--name", name, "--label", "oma.verify-chat.run="+os.Getenv("VERIFY_CHAT_RUN_ID"), "--cap-add", "SYS_ADMIN", "--device", "/dev/fuse", "--security-opt", "apparmor=unconfined", "--add-host", "host.docker.internal:host-gateway", "--entrypoint", "sleep", p.image, "infinity").Run()
	return e2bruntime.Sandbox{ID: name}, err
}
func (p *chatDockerProvider) Kill(ctx context.Context, id string) error {
	return exec.CommandContext(ctx, "docker", "rm", "-f", id).Run()
}
func (p *chatDockerProvider) WriteFile(ctx context.Context, id, path string, data []byte) error {
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", id, "sh", "-c", `mkdir -p "$(dirname "$1")" && cat > "$1"`, "sh", path)
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Run()
}
func (p *chatDockerProvider) FileExists(ctx context.Context, id, path string) (bool, error) {
	err := exec.CommandContext(ctx, "docker", "exec", id, "test", "-e", path).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return err == nil, err
}
func (p *chatDockerProvider) RunCommand(ctx context.Context, id string, request e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", id, "sh", "-c", request.Command)
	cmd.Stdin = bytes.NewReader(request.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	result := e2bruntime.CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if errors.As(err, &exit) {
		result.ExitCode = exit.ExitCode()
		err = nil
	}
	return result, err
}
func (p *chatDockerProvider) StartBackgroundCommand(ctx context.Context, id, command string, stdin []byte) error {
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", id, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(stdin)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	p.t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	return nil
}

func TestChatPublicStart(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	previewSeen := make(chan struct{}, 2)
	var calls atomic.Int32
	model := chatModelFixture(t, previewSeen, &calls)
	configureChatModel(t, e, model.URL)
	cfg, err := config.Load()
	requireOK(t, err)
	target, err := url.Parse(e.url)
	requireOK(t, err)
	cfg.CodeSession.SandboxAPIBaseURL = serveRealWorkerFixture(t, httputil.NewSingleHostReverseProxy(target))
	cfg.CodeSession.UpstreamProxyMITMEnabled = false
	provider := &chatDockerProvider{name: "oma-public-" + os.Getenv("VERIFY_CHAT_RUN_ID"), image: os.Getenv("OMA_WORKER_CONTROL_IMAGE"), t: t}
	cfg.EnvironmentRunner.Enabled = true
	cfg.EnvironmentRunner.Concurrency = 2
	credentials, err := filestore.NewTokenCredentials(cfg)
	requireOK(t, err)
	runner, err := environments.NewRunner(environments.RunnerDependencies{DB: e.database, Provider: provider, Config: cfg, CodeSessions: e.service, Skills: skillsapi.NewRuntimeResolver(e.database), FilestoreTokens: credentials})
	requireOK(t, err)
	stopRunner := runner.Start(t.Context())
	defer stopRunner()
	provider.failNext.Store(true)
	failed := createPublicChat(t, e)
	waitRealWorker(t, "failed work settled by background runner", func() bool {
		work, err := e.database.GetLatestEnvironmentWorkForSession(t.Context(), e.key.WorkspaceUUID.String(), e.environment.ExternalID, failed.session.UUID)
		requireOK(t, err)
		return work.State == "stopped"
	})
	chatProof(t, started, "public_runner_error_recovered")
	for i := 0; i < 2; i++ {
		verifyPublicTurn(t, e, previewSeen)
	}
	if calls.Load() != 2 || provider.created.Load() != 2 {
		t.Fatalf("model requests=%d sandbox allocations=%d, want two each", calls.Load(), provider.created.Load())
	}
	chatProof(t, started, "public_session_started")
	chatProof(t, started, "public_runner_roundtrip")
	chatProof(t, started, "public_runner_multiple_work")
}

func createPublicChat(t *testing.T, e *liveEnv) *liveSession {
	t.Helper()
	var session struct {
		ID string `json:"id"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/sessions", e.apiKey, map[string]string{"agent": e.agent.ExternalID, "environment_id": e.environment.ExternalID}, 200), &session))
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "DELETE", "/v1/sessions/"+session.ID, e.apiKey, nil, 200)
	})
	record, _, err := e.database.GetSession(t.Context(), e.key.WorkspaceUUID.String(), session.ID)
	requireOK(t, err)
	if record.Title != nil {
		t.Fatalf("new session title = %q, want nil", *record.Title)
	}
	return &liveSession{env: e, session: record}
}

func verifyPublicTurn(t *testing.T, e *liveEnv, previewSeen chan struct{}) {
	t.Helper()
	f := createPublicChat(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 90*time.Second))
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	input := submitChat(t, f, "请验证公开启动路径。")
	final := receiveChatAnswer(t, ctx, events, func() { previewSeen <- struct{}{} })
	var err error
	f.code, err = e.database.GetCodeSessionBySessionExternalID(ctx, e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	verifyChatHistory(t, f, input, final)
	waitChatIdle(t, f, 1)
	sandbox, err := e.database.GetResumableEnvironmentSandboxForCodeSession(ctx, f.code.ExternalID)
	requireOK(t, err)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		now := time.Now().UTC()
		if err := e.database.UpdateEnvironmentSandboxState(cleanupCtx, sandbox.WorkspaceUUID, sandbox.ExternalID, "stopped", sandbox.ProviderSandboxID, nil, &now); err != nil {
			t.Error(err)
		}
		if sandbox.WorkExternalID != nil {
			if _, err := e.database.StopEnvironmentWork(cleanupCtx, sandbox.WorkspaceUUID, e.environment.ExternalID, *sandbox.WorkExternalID, true); err != nil {
				t.Error(err)
			}
		}
	})
}

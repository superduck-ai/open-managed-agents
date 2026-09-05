//go:build e2e

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	e2b "github.com/superduck-ai/e2b-go-sdk"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

// This is a real Sandbox -> Environment Manager -> WebSocket CONNECT -> TLS
// MITM -> private GitHub integration. Build the test image with the companion
// fixtures/git_resources_claude.sh executable at /usr/local/bin/oma-git-test-agent.
// This deterministic agent also exercises Git with the inherited relay environment,
// without LLM credentials or inference.
func TestGitResourcesRuntimeE2E(t *testing.T) {
	repoURL := os.Getenv("OMA_GIT_E2E_REPOSITORY")
	if repoURL == "" {
		t.Skip("set OMA_GIT_E2E_REPOSITORY to a private https://github.com/owner/repo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	token := os.Getenv("OMA_GIT_E2E_TOKEN")
	if token == "" && os.Getenv("OMA_GIT_E2E_USE_GH_AUTH") == "1" {
		raw, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
		if err != nil {
			t.Fatal("unable to read explicitly enabled gh credential")
		}
		token = strings.TrimSpace(string(raw))
		clear(raw)
	}
	if token == "" {
		t.Fatal("OMA_GIT_E2E_TOKEN or OMA_GIT_E2E_USE_GH_AUTH=1 is required")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	requireFullE2BBridgeConfig(t, cfg)
	if !quickstartLooksLikeLoopbackURL(cfg.E2B.APIURL) {
		t.Fatal("Git resource runtime E2E requires a local E2B gateway reachable on loopback")
	}
	if !cfg.CodeSession.UpstreamProxyMITMEnabled {
		t.Fatal("real Git resource test requires MITM enabled")
	}
	cfg.EnvironmentRunner.ClaudePath = "/usr/local/bin/oma-git-test-agent"
	cfg.EnvironmentRunner.ClaudeAgentVersion = "2.1.251"
	cfg.CodeSession.SandboxAPIBaseURL = ""
	var runtimeLog bytes.Buffer
	store, cfg := newS3ObjectStore(t, &cfg)
	logger := slog.New(slog.NewTextHandler(&runtimeLog, &slog.HandlerOptions{Level: slog.LevelWarn}))
	app := newTestAppWithStoreAndLogger(t, &cfg, store, logger)
	defer func() {
		app.close()
		if t.Failed() {
			t.Log(strings.ReplaceAll(runtimeLog.String(), token, "[REDACTED]"))
		}
	}()
	quickstartEnsureSandboxIngress(t, app)
	cfg = app.cfg
	agent := createAgent(t, app, `{"name":"Git runtime E2E","model":"claude-opus-4-6"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	environment := createEnvironment(t, app, fmt.Sprintf(`{"name":%q,"config":{"type":"cloud","networking":{"type":"unrestricted"}}}`, "git-runtime-"+time.Now().Format("150405.000000000")))
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	// An anonymous clone must be denied, otherwise success cannot prove server-side
	// credential injection. No token is sent by this request.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repoURL+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("anonymous GitHub probe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("repository must deny anonymous clone; status=%d", resp.StatusCode)
	}
	var resolvedSHA, defaultBranch string
	for _, name := range []string{"session default branch", "session explicit branch", "deployment pinned commit"} {
		passed := t.Run(name, func(t *testing.T) {
			resource := map[string]any{"type": "github_repository", "url": repoURL, "authorization_token": token, "mount_path": "/workspace/source"}
			if name == "deployment pinned commit" {
				resource["checkout"] = map[string]string{"type": "commit", "sha": resolvedSHA}
			} else if name == "session explicit branch" {
				resource["checkout"] = map[string]string{"type": "branch", "name": defaultBranch}
			}
			raw, err := json.Marshal([]any{resource})
			if err != nil {
				t.Fatal(err)
			}
			var sessionID string
			if name != "deployment pinned commit" {
				session := createSession(t, app, fmt.Sprintf(`{"agent":%q,"environment_id":%q,"resources":%s}`, agent.ID, environment.ID, raw))
				sessionID = session.ID
				sendSessionEvents(t, app, sessionID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"Initialize the repository."}]}]}`, defaultTestKey)
			} else {
				deployment := createDeployment(t, app, deploymentBodyWithExtra(agent.ID, environment.ID, `"resources":`+string(raw)))
				defer cleanupDeploymentRows(t, app, deployment.ID)
				run := runDeployment(t, app, deployment.ID)
				if run.SessionID == nil {
					t.Fatal("deployment did not create a session")
				}
				sessionID = *run.SessionID
			}
			clear(raw)
			defer deleteSession(t, app, sessionID)
			workID := quickstartFindSessionEnvironmentWorkID(t, app, environment.ID, sessionID)
			provider := e2bruntime.NewProvider(cfg.E2B)
			runner := newManagedAgentRunner(t, app, provider, cfg)
			processed, err := runner.RunOnce(ctx, "git-resource-e2e")
			if err != nil {
				t.Fatalf("runner launch: %v", err)
			}
			if !processed {
				t.Fatal("runner found no session work")
			}
			sandboxID, _ := quickstartWaitForProviderSandboxMetadata(t, ctx, app, environment.ID, workID)
			defer func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), time.Minute)
				defer stop()
				if err := provider.Kill(cleanupCtx, sandboxID); err != nil {
					t.Errorf("cleanup sandbox: %v", err)
				}
			}()
			sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{ConnectionOpts: e2bruntime.ConnectionOptsFromConfig(cfg.E2B)})
			if err != nil {
				t.Fatal(err)
			}
			sha := waitForGitResourceCheckout(t, ctx, sandbox, token, repoURL)
			assertAgentGitProxyResult(t, ctx, sandbox, "ok")
			if name == "session default branch" {
				verifyGitProxyRotation(t, ctx, app, sandbox, sessionID, token)
			}
			if name == "deployment pinned commit" && sha != resolvedSHA {
				t.Fatalf("checkout HEAD=%s want=%s", sha, resolvedSHA)
			}
			if resolvedSHA == "" {
				resolvedSHA = sha
				branch, _, err := runE2BCommand(ctx, sandbox, "git -C /workspace/source symbolic-ref --short HEAD", 10*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defaultBranch = strings.TrimSpace(branch)
			}
			t.Logf("verified private repository clone: entry=%s template=%s HEAD=%s depth=1 mount=/workspace/source", name, cfg.E2B.Template, sha)
		})
		if !passed {
			return
		}
	}
}

func waitForGitResourceCheckout(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, token, repoURL string) string {
	t.Helper()
	deadline := time.Now().Add(100 * time.Second)
	const probe = `if test -f /workspace/source/.git/shallow; then git -C /workspace/source rev-parse HEAD; git -C /workspace/source rev-list --count HEAD; git -C /workspace/source remote get-url origin; git -C /workspace/source status --porcelain >/dev/null; elif grep -qs "^Error: environment manager failed:" /tmp/claude-code-sessions/*/environment-manager.log; then echo manager_failed; fi`
	for time.Now().Before(deadline) {
		stdout, stderr, err := runE2BCommand(ctx, sandbox, probe, 10*time.Second)
		if strings.Contains(stdout, token) || strings.Contains(stderr, token) {
			t.Fatal("repository probe exposed GitHub credential")
		}
		if strings.TrimSpace(stdout) == "manager_failed" {
			break
		}
		fields := strings.Split(strings.TrimSpace(stdout), "\n")
		if err == nil && len(fields) == 3 && len(fields[0]) >= 40 && fields[1] == "1" && fields[2] == repoURL {
			// No plaintext token or credential-bearing URL may enter persisted Git config.
			configText, _, readErr := runE2BCommand(ctx, sandbox, "cat /workspace/source/.git/config", 10*time.Second)
			if readErr != nil {
				t.Fatal("read cloned Git config")
			}
			if strings.Contains(configText, token) || strings.Contains(configText, "authorization") || strings.Contains(configText, "extraheader") {
				t.Fatal("Git config contains credentials")
			}
			return fields[0]
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Second):
		}
	}
	// Only print a bounded, redacted process log. Never expose the runtime payload.
	stdout, _, _ := runE2BCommand(ctx, sandbox, `find /tmp/claude-code-sessions -name environment-manager.log -exec tail -n 35 {} \;`, 10*time.Second)
	stdout = strings.ReplaceAll(stdout, token, "[REDACTED]")
	t.Fatalf("private shallow checkout did not become ready:\n%s", stdout)
	return ""
}

// Reuse the already running child and CONNECT relay while rotating only the API
// resource credential. A bad token must fail and a restored token must succeed.
func verifyGitProxyRotation(t *testing.T, ctx context.Context, app *testApp, sandbox *e2b.Sandbox, sessionID, token string) {
	t.Helper()
	row := mustGitResourceRows(t, app, sessionID)[0]
	for _, step := range []struct{ token, result string }{{"invalid-git-e2e-token", "denied"}, {token, "ok"}} {
		raw, err := json.Marshal(map[string]string{"authorization_token": step.token})
		if err != nil {
			t.Fatal(err)
		}
		response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"/resources/"+row.ExternalID+"?beta=true", strings.NewReader(string(raw)), defaultTestKey, true)
		clear(raw)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("rotate runtime token: status %d", response.StatusCode)
		}
		if _, _, err := runE2BCommand(ctx, sandbox, "rm -f /workspace/source/.git/oma-proxy-result && touch /workspace/source/.git/oma-proxy-check", 10*time.Second); err != nil {
			t.Fatal(err)
		}
		assertAgentGitProxyResult(t, ctx, sandbox, step.result)
	}
}

func assertAgentGitProxyResult(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, expected string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		stdout, _, err := runE2BCommand(ctx, sandbox, "cat /workspace/source/.git/oma-proxy-result 2>/dev/null || true", 5*time.Second)
		if err == nil && strings.TrimSpace(stdout) != "" {
			if strings.TrimSpace(stdout) != expected {
				t.Fatalf("agent Git proxy result=%q, expected=%q", strings.TrimSpace(stdout), expected)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("agent Git proxy probe did not complete")
}

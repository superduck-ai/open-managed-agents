//go:build e2b_integration && e2e

package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	e2b "github.com/superduck-ai/e2b-go-sdk"
)

func TestE2BManagedAgentBridgeEnvironmentManagerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real E2B managed-agent bridge integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	requireFullE2BBridgeConfig(t, cfg)
	cfg.CodeSessionSandboxAPIBaseURL = ""
	cfg.E2BTemplate = config.DefaultE2BTemplate
	if cfg.E2BRequestTimeout < 2*time.Minute {
		cfg.E2BRequestTimeout = 2 * time.Minute
	}
	if cfg.E2BSandboxTimeout < 15*time.Minute {
		cfg.E2BSandboxTimeout = 15 * time.Minute
	}

	app := newTestAppWithStore(t, &cfg, newFakeStore("full-e2b-bridge-bucket"))
	defer app.close()
	quickstartEnsureSandboxIngress(t, app)
	cfg = app.cfg

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)

	agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name: "Full E2B Bridge Agent",
		Model: anthropic.BetaManagedAgentsModelConfigParams{
			ID: anthropic.BetaManagedAgentsModelClaudeOpus4_8,
		},
		System: anthropic.String("You are a concise coding assistant."),
		Tools: []anthropic.BetaAgentNewParamsToolUnion{{
			OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
				Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
			},
		}},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	defer client.Beta.Agents.Archive(context.Background(), agent.ID, anthropic.BetaAgentArchiveParams{})

	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: "full-e2b-bridge-" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""),
		Config: anthropic.BetaEnvironmentNewParamsConfigUnion{
			OfCloud: &anthropic.BetaCloudConfigParams{
				Networking: anthropic.BetaCloudConfigParamsNetworkingUnion{
					OfUnrestricted: &anthropic.BetaUnrestrictedNetworkParam{},
				},
				Packages: anthropic.BetaPackagesParams{
					Type:  anthropic.BetaPackagesParamsTypePackages,
					Apt:   []string{"ffmpeg"},
					Cargo: []string{"ripgrep@14.1.1"},
					Gem:   []string{"rake:13.2.1"},
					Go: []string{
						"golang.org/x/tools/cmd/goimports@v0.35.0",
						"github.com/google/addlicense@v1.1.1",
					},
					Npm: []string{"typescript@5.9.3"},
					Pip: []string{"numpy==2.3.5"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	defer client.Beta.Environments.Delete(context.Background(), environment.ID, anthropic.BetaEnvironmentDeleteParams{})

	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		Title:         anthropic.String("Full E2B bridge session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})
	const agentPrompt = "Run rg --version and write only its first output line to /home/user/package-agent-proof.txt. Do not install any package."
	sendManagedAgentUserMessage(t, ctx, client, session.ID, agentPrompt)

	workID := quickstartFindSessionEnvironmentWorkID(t, app, environment.ID, session.ID)

	provider := e2bruntime.NewProvider(cfg)
	var providerSandboxID string
	stopped := false
	defer func() {
		if providerSandboxID != "" && !stopped {
			killCtx, cancelKill := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancelKill()
			_ = provider.Kill(killCtx, providerSandboxID)
		}
	}()

	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, nil, app.credentials)
	processed, err := runner.RunOnce(ctx, "full-e2b-bridge-test")
	if err != nil {
		t.Fatalf("run environment runner once: %v", err)
	}
	if !processed {
		t.Fatal("environment runner did not process queued session work")
	}

	codeSessionID, metadata := quickstartWaitForCodeSessionMetadata(t, ctx, app, session.ID)
	if strings.TrimSpace(codeSessionID) == "" || metadata["runtime"] != "claude_code_local" {
		t.Fatalf("session metadata was not patched with local code session ids: %#v", metadata)
	}

	providerSandboxID, workState := quickstartWaitForProviderSandboxMetadata(t, ctx, app, environment.ID, workID)
	if workState != "active" && workState != "running" {
		t.Fatalf("environment work state = %s, want active", workState)
	}
	if strings.TrimSpace(providerSandboxID) == "" {
		t.Fatal("provider sandbox id was not recorded")
	}

	sandbox, err := e2b.Connect(ctx, providerSandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bConnectionOptsFromConfig(cfg),
	})
	if err != nil {
		t.Fatalf("connect to real sandbox %s: %v", providerSandboxID, err)
	}

	probe := waitForEnvironmentManagerProcess(t, ctx, sandbox, codeSessionID)
	t.Logf("environment-manager started for code session %s in sandbox %s:\n%s", codeSessionID, providerSandboxID, probe)
	packageProbe := probeInstalledPackages(t, ctx, sandbox)
	t.Logf("all environment packages are usable before/during agent runtime:\n%s", packageProbe)
	agentProbe := waitForPackageAgentProof(t, ctx, sandbox)
	t.Logf("Claude Agent used the installed package:\n%s", agentProbe)
	transcript, err := quickstartWaitForSessionTranscript(t, ctx, app, session.ID, 2*time.Minute)
	if err != nil {
		t.Fatalf("wait for Claude Agent transcript: %v", err)
	}
	if !strings.Contains(transcript, "Agent finished.") {
		t.Fatalf("Claude Agent did not reach idle after using installed package:\n%s", transcript)
	}

	quickstartStopEnvironmentWork(t, ctx, app, environment.ID, workID)
	stopped = true
}

func sendManagedAgentUserMessage(t *testing.T, ctx context.Context, client anthropic.Client, sessionID, prompt string) {
	t.Helper()
	sent, err := client.Beta.Sessions.Events.Send(ctx, sessionID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
					OfText: &anthropic.BetaManagedAgentsTextBlockParam{
						Type: anthropic.BetaManagedAgentsTextBlockTypeText,
						Text: prompt,
					},
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("send managed-agent user message: %v", err)
	}
	if len(sent.Data) != 1 || sent.Data[0].Type != "user.message" {
		t.Fatalf("unexpected managed-agent send response: %+v", sent.Data)
	}
}

func waitForPackageAgentProof(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox) string {
	t.Helper()
	const command = `
set +e
if [ -f /home/user/package-agent-proof.txt ]; then
  printf 'package_agent_proof='
  cat /home/user/package-agent-proof.txt
fi
printf '\n--- processes ---\n'
ps -eo pid=,args=ww | grep -E '[e]nvironment-manager task-run|[/]opt/claude-code/bin/claude' || true
`
	deadline := time.Now().Add(4 * time.Minute)
	var last string
	for {
		stdout, stderr, err := runE2BCommand(ctx, sandbox, command, 30*time.Second)
		last = strings.TrimSpace(stdout + "\n" + stderr)
		if err == nil && strings.Contains(stdout, "package_agent_proof=ripgrep 14.1.1") {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("Claude Agent did not create package proof with installed ripgrep: %v\n%s", err, last)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for Claude Agent package proof: %v\n%s", ctx.Err(), last)
		case <-time.After(5 * time.Second):
		}
	}
}

func probeInstalledPackages(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox) string {
	t.Helper()
	command := `
set -eu
ffmpeg -version | head -n 1
rg --version | head -n 1
ruby -e 'gem "rake", "=13.2.1"; require "rake"; puts "rake #{Rake::VERSION}"'
	goimports -h >/dev/null 2>&1 || [ "$?" -eq 2 ]
	printf 'goimports available\n'
	command -v addlicense >/dev/null
	printf 'addlicense available\n'
tsc --version
python3 -c 'import numpy; assert numpy.__version__ == "2.3.5"; print("numpy " + numpy.__version__)'
`
	stdout, stderr, err := runE2BCommand(ctx, sandbox, command, 2*time.Minute)
	if err != nil {
		t.Fatalf("probe installed packages: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	return strings.TrimSpace(stdout + "\n" + stderr)
}

func requireFullE2BBridgeConfig(t *testing.T, cfg config.Config) {
	t.Helper()
	if strings.TrimSpace(cfg.E2BAPIKey) == "" && !cfg.E2BDebug {
		t.Skip("E2B_API_KEY is required in the current .env for this real integration test")
	}
	if cfg.E2BDebug {
		t.Skip("E2B_DEBUG must be false for this real integration test")
	}
	if baseURL := quickstartConfiguredSandboxIngressBaseURL(cfg); quickstartLooksLikeLoopbackURL(baseURL) {
		t.Fatalf("code session ingress URL used inside E2B must be reachable from inside E2B, got %q", baseURL)
	}
}

func waitForEnvironmentManagerProcess(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, codeSessionID string) string {
	t.Helper()
	logPath := "/tmp/claude-code-sessions/" + sandboxSafeCodeSessionID(codeSessionID) + "/environment-manager.log"
	command := fmt.Sprintf(`
ps -eo pid=,args=ww | grep '[e]nvironment-manager task-run' || true
printf '%s\n' '--- environment-manager log ---'
if [ -f %[1]s ]; then tail -n 120 %[1]s; else printf 'log file missing: %[1]s\n'; fi
`, shellPath(logPath))

	deadline := time.Now().Add(75 * time.Second)
	var last string
	for {
		stdout, stderr, err := runE2BCommand(ctx, sandbox, command, 30*time.Second)
		last = strings.TrimSpace(stdout + "\n" + stderr)
		if err == nil && strings.Contains(stdout, "environment-manager task-run") && strings.Contains(stdout, codeSessionID) {
			return last
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("environment-manager process did not appear for %s; last probe error: %v\n%s", codeSessionID, err, last)
			}
			t.Fatalf("environment-manager process did not appear for %s\n%s", codeSessionID, last)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for environment-manager process: %v\n%s", ctx.Err(), last)
		case <-time.After(2 * time.Second):
		}
	}
}

func runE2BCommand(ctx context.Context, sandbox *e2b.Sandbox, command string, timeout time.Duration) (string, string, error) {
	timeoutMs := int(timeout / time.Millisecond)
	execution, err := sandbox.Commands.Run(ctx, command, &e2b.CommandStartOpts{TimeoutMs: &timeoutMs})
	if err != nil {
		return "", "", err
	}
	result, ok := execution.(*e2b.CommandResult)
	if !ok {
		return "", "", fmt.Errorf("command execution type = %T, want *e2b.CommandResult", execution)
	}
	if result.ExitCode != 0 {
		return result.Stdout, result.Stderr, fmt.Errorf("command exited with code %d: %s", result.ExitCode, strings.TrimSpace(result.Error))
	}
	return result.Stdout, result.Stderr, nil
}

func sandboxSafeCodeSessionID(codeSessionID string) string {
	return strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(codeSessionID)
}

func shellPath(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

//go:build e2e

package tests

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	e2b "github.com/superduck-ai/e2b-go-sdk"
)

const fullE2BManagedAgentSandboxImage = "registry.gz.cvte.cn/oma/managed-agent-sandbox:latest"

func TestE2BManagedAgentBridgeEnvironmentManagerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real E2B managed-agent bridge integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.E2B.Template = fullE2BManagedAgentSandboxImage
	requireFullE2BBridgeConfig(t, cfg)
	t.Logf("Testing managed-agent sandbox image %s", cfg.E2B.Template)
	if cfg.E2B.RequestTimeout < 2*time.Minute {
		cfg.E2B.RequestTimeout = 2 * time.Minute
	}
	if cfg.E2B.SandboxTimeout < 2*time.Minute {
		cfg.E2B.SandboxTimeout = 2 * time.Minute
	}

	// The sandbox reaches Filestore through the configured external ingress,
	// so the test app and that ingress must share the configured object store.
	app := newTestApp(t, &cfg)
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
	var uploadedFileID string
	defer func() {
		if _, deleteErr := client.Beta.Sessions.Delete(
			context.Background(),
			session.ID,
			anthropic.BetaSessionDeleteParams{},
		); deleteErr != nil {
			t.Errorf("delete E2B session during cleanup: %v", deleteErr)
			return
		}
		if uploadedFileID != "" {
			deleteFile(t, app, uploadedFileID)
		}
	}()

	file := uploadFile(t, app, "e2b-data.csv", "text/csv", []byte("name,value\nalpha,1\n"))
	uploadedFileID = file.ID
	resourceResponse := doSessionRequest(
		t,
		app,
		http.MethodPost,
		"/v1/sessions/"+session.ID+"/resources?beta=true",
		strings.NewReader(fmt.Sprintf(`{"type":"file","file_id":%q,"mount_path":"/workspace/data.csv"}`, file.ID)),
		defaultTestKey,
		true,
	)
	defer resourceResponse.Body.Close()
	if resourceResponse.StatusCode != http.StatusOK {
		t.Fatalf("add E2B file resource status = %d: %s", resourceResponse.StatusCode, readAll(t, resourceResponse.Body))
	}

	workID := quickstartFindSessionEnvironmentWorkID(t, app, environment.ID, session.ID)

	provider := e2bruntime.NewProvider(cfg.E2B)
	var providerSandboxID string
	stopped := false
	defer func() {
		if providerSandboxID != "" && !stopped {
			killCtx, cancelKill := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancelKill()
			_ = provider.Kill(killCtx, providerSandboxID)
		}
	}()

	runner := newManagedAgentRunner(app, provider, cfg, nil)
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
	sandboxRecord, err := app.db.GetActiveEnvironmentSandboxForWork(
		ctx,
		getDefaultDBIDs(t, app.db).WorkspaceID,
		environment.ID,
		workID,
	)
	if err != nil {
		t.Fatalf("load active E2B sandbox record: %v", err)
	}
	if sandboxRecord.Template != fullE2BManagedAgentSandboxImage {
		t.Fatalf(
			"E2B sandbox template = %q, want %q",
			sandboxRecord.Template,
			fullE2BManagedAgentSandboxImage,
		)
	}

	sandbox, err := e2b.Connect(ctx, providerSandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bruntime.ConnectionOptsFromConfig(cfg.E2B),
	})
	if err != nil {
		t.Fatalf("connect to real sandbox %s: %v", providerSandboxID, err)
	}

	assertE2BFilestoreMounts(t, ctx, sandbox)
	probe := waitForEnvironmentManagerProcess(t, ctx, sandbox, codeSessionID)
	t.Logf("environment-manager started for code session %s in sandbox %s:\n%s", codeSessionID, providerSandboxID, probe)

	quickstartStopEnvironmentWork(t, ctx, app, environment.ID, workID)
	stopped = true
}

func assertE2BFilestoreMounts(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox) {
	t.Helper()
	command := `
set -eu
test -x /opt/rclone/rclone-filestore
test ! -e /tmp/rclone-mount-config.json
test "$(cat /mnt/session/uploads/workspace/data.csv)" = "name,value
alpha,1"
printf 'output-ok\n' > /mnt/user-data/outputs/e2b-output.txt
test "$(cat /mnt/user-data/outputs/e2b-output.txt)" = "output-ok"
for target in \
	/mnt/session/uploads/e2b-readonly-test \
	/mnt/transcripts/e2b-readonly-test \
	/mnt/user-data/tool_results/e2b-readonly-test \
	/mnt/session/uploads/workspace/data.csv
do
	if printf 'must-fail\n' > "$target"; then
		echo "readonly write unexpectedly succeeded: $target" >&2
		exit 1
	fi
done
printf 'e2b-filestore-ok\n'
`
	stdout, stderr, err := runE2BCommand(ctx, sandbox, command, 30*time.Second)
	if err != nil {
		t.Fatalf("verify real E2B filestore mounts: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "e2b-filestore-ok") {
		t.Fatalf("real E2B filestore probe did not finish:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

func requireFullE2BBridgeConfig(t *testing.T, cfg config.Config) {
	t.Helper()
	if !quickstartShouldRunRealSandbox(cfg) {
		t.Skip("hosted E2B credentials or a complete local E2B gateway configuration is required for this real integration test")
	}
	quickstartRequireRealSandboxConfig(t, cfg)
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

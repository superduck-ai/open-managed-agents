//go:build e2e

package tests

import (
	"context"
	"encoding/json"
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
	if quickstartLooksLikeLoopbackURL(cfg.E2B.APIURL) {
		app.cfg.CodeSession.SandboxAPIBaseURL = ""
	}
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
		}
		if uploadedFileID != "" {
			deleteFile(t, app, uploadedFileID)
		}
	}()

	file := uploadFile(t, app, "config.example.yaml", "text/yaml", []byte("value=41\n"))
	uploadedFileID = file.ID
	resourceResponse := doSessionRequest(
		t,
		app,
		http.MethodPost,
		"/v1/sessions/"+session.ID+"/resources?beta=true",
		strings.NewReader(fmt.Sprintf(`{"type":"file","file_id":%q,"mount_path":"/config.example.yaml"}`, file.ID)),
		defaultTestKey,
		true,
	)
	defer resourceResponse.Body.Close()
	if resourceResponse.StatusCode != http.StatusOK {
		t.Fatalf("add E2B file resource status = %d: %s", resourceResponse.StatusCode, readAll(t, resourceResponse.Body))
	}

	sendText := func(text string) string {
		t.Helper()
		response, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
			Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
				OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
					Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
					Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
						OfText: &anthropic.BetaManagedAgentsTextBlockParam{
							Type: anthropic.BetaManagedAgentsTextBlockTypeText,
							Text: text,
						},
					}},
				},
			}},
		})
		if err != nil {
			t.Fatalf("send E2B file task: %v", err)
		}
		if len(response.Data) != 1 {
			t.Fatalf("sent session events = %d, want 1", len(response.Data))
		}
		return response.Data[0].ID
	}
	firstMessageID := sendText("帮我看下 config.example.yaml 都有什么内容？")

	workID := quickstartFindSessionEnvironmentWorkID(t, app, environment.ID, session.ID)

	provider := e2bruntime.NewProvider(cfg.E2B)
	var providerSandboxID string
	stopped := false
	defer func() {
		if !stopped {
			stopCtx, cancelStop := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancelStop()
			quickstartStopEnvironmentWork(t, stopCtx, app, environment.ID, workID)
			stopped = true
		}
	}()

	runner := newManagedAgentRunner(t, app, provider, cfg)
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
		getDefaultDBIDs(t, app.pool).WorkspaceUUID,
		environment.ID,
		workID,
	)
	if err != nil {
		t.Fatalf("load active E2B sandbox record: %v", err)
	}
	if sandboxRecord.Template != cfg.E2B.Template {
		t.Fatalf(
			"E2B sandbox template = %q, want %q",
			sandboxRecord.Template,
			cfg.E2B.Template,
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
	waitForAgentMessageContaining(t, ctx, app, session.ID, firstMessageID, "value=41")
	sendText("把刚才读取到的 value 值加 1，只把结果和一个换行写到用户可下载的 result.txt。")
	waitForManagedAgentOutput(t, ctx, sandbox, "/mnt/user-data/outputs/result.txt")
	output := waitForSessionOutputFile(t, ctx, app, session.ID, "result.txt")
	if !output.Downloadable {
		t.Fatalf("output file %s is not downloadable: %+v", output.ID, output)
	}
	response := app.do(t, http.MethodGet, "/v1/files/"+output.ID+"/content?beta=true", nil, defaultTestKey, true, "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download output status = %d: %s", response.StatusCode, readAll(t, response.Body))
	}
	if downloaded := readAll(t, response.Body); string(downloaded) != "42\n" {
		t.Fatalf("downloaded output = %q, want %q", downloaded, "42\\n")
	}
	t.Logf("Agent output %s was cataloged as %s and downloaded successfully", output.Filename, output.ID)

	quickstartStopEnvironmentWork(t, ctx, app, environment.ID, workID)
	stopped = true
}

func assertE2BFilestoreMounts(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox) {
	t.Helper()
	command := `
set -eu
test -x /opt/rclone/rclone-filestore
test ! -e /tmp/rclone-mount-config.json
test "$(cat /mnt/session/uploads/config.example.yaml)" = "value=41"
printf 'output-ok\n' > /mnt/user-data/outputs/e2b-output.txt
test "$(cat /mnt/user-data/outputs/e2b-output.txt)" = "output-ok"
for target in \
	/mnt/session/uploads/e2b-readonly-test \
	/mnt/transcripts/e2b-readonly-test \
	/mnt/user-data/tool_results/e2b-readonly-test \
	/mnt/session/uploads/config.example.yaml
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

func waitForManagedAgentOutput(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, outputPath string) {
	t.Helper()
	command := "test -f " + shellPath(outputPath)
	deadline := time.Now().Add(4 * time.Minute)
	var lastErr error
	for {
		_, _, lastErr = runE2BCommand(ctx, sandbox, command, 30*time.Second)
		if lastErr == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed agent did not create %s: %v", outputPath, lastErr)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for managed agent output %s: %v", outputPath, ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}

func waitForAgentMessageContaining(t *testing.T, ctx context.Context, app *testApp, sessionID, afterEventID, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		page := listSessionEvents(t, app, sessionID, "order=asc&limit=1000", defaultTestKey)
		var messages []string
		boundarySeen := false
		started := false
		finished := false
		for _, raw := range page.Data {
			var event map[string]any
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatalf("decode session event: %v", err)
			}
			if event["id"] == afterEventID {
				boundarySeen = true
				continue
			}
			if !boundarySeen {
				continue
			}
			switch event["type"] {
			case "agent.message":
				started = true
				text := rawAgentMessageText(event)
				messages = append(messages, text)
				if strings.Contains(text, want) {
					return
				}
			case "agent.thinking", "agent.tool_use", "agent.tool_result", "agent.mcp_tool_use", "agent.mcp_tool_result":
				started = true
			case "session.status_idle":
				finished = started && event["stop_reason"] != nil
			}
		}
		if finished {
			t.Fatalf("agent messages = %q, want one to contain %q", messages, want)
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed agent did not answer with %q; messages = %q", want, messages)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for managed agent response: %v", ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

func rawAgentMessageText(event map[string]any) string {
	texts := make([]string, 0)
	for _, block := range quickstartContentBlocks(event) {
		if block["type"] == "text" {
			texts = append(texts, quickstartStringValue(block["text"]))
		}
	}
	return strings.Join(texts, "\n")
}

func waitForSessionOutputFile(t *testing.T, ctx context.Context, app *testApp, sessionID, filename string) metadataResponse {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		page := listFiles(t, app, "scope_id="+sessionID+"&limit=100")
		for _, file := range page.Data {
			if file.Filename == filename {
				return file
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("session output %q did not enter the Files API catalog: %+v", filename, page.Data)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for session output %q in Files API: %v", filename, ctx.Err())
		case <-time.After(time.Second):
		}
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

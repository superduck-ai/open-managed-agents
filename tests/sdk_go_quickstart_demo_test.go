//go:build e2e

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
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

func TestGoSDKManagedAgentsQuickstartDemo(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !quickstartShouldRunRealSandbox(cfg) {
		t.Skip("real quickstart requires E2B_API_KEY and E2B_DEBUG=false")
	}
	//if externalBaseURL := strings.TrimSpace(os.Getenv("TEST_API_BASE_URL")); externalBaseURL != "" {
	//	t.Logf("Ignoring TEST_API_BASE_URL=%s; this test starts its own in-process API server so it can run the environment runner and inspect E2B sandbox state", externalBaseURL)
	//}
	quickstartRequireRealSandboxConfig(t, cfg)
	if cfg.E2BRequestTimeout < 2*time.Minute {
		cfg.E2BRequestTimeout = 2 * time.Minute
	}
	if cfg.E2BSandboxTimeout < 2*time.Minute {
		cfg.E2BSandboxTimeout = 2 * time.Minute
	}

	app := newTestAppWithStore(t, &cfg, newFakeStore("quickstart-demo-bucket"))
	defer app.close()

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name: "Coding Assistant",
		Model: anthropic.BetaManagedAgentsModelConfigParams{
			ID: anthropic.BetaManagedAgentsModelClaudeOpus4_8,
		},
		System: anthropic.String("You are a helpful coding assistant. Write clean, well-documented code."),
		Tools: []anthropic.BetaAgentNewParamsToolUnion{{
			OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
				Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
			},
		}},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Logf("Agent ID: %s, version: %d", agent.ID, agent.Version)
	defer client.Beta.Agents.Archive(context.Background(), agent.ID, anthropic.BetaAgentArchiveParams{})

	environmentName := fmt.Sprintf("quickstart-env-%d", time.Now().UnixNano())
	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: environmentName,
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
	t.Logf("Environment ID: %s", environment.ID)
	defer client.Beta.Environments.Delete(context.Background(), environment.ID, anthropic.BetaEnvironmentDeleteParams{})

	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		Title:         anthropic.String("Quickstart session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Logf("Session ID: %s", session.ID)
	t.Logf("Leaving session %s undeleted for inspection after the quickstart run", session.ID)

	stream := client.Beta.Sessions.Events.StreamEvents(ctx, session.ID, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()

	type streamResult struct {
		event anthropic.BetaManagedAgentsStreamSessionEventsUnion
		err   error
	}
	firstEvent := make(chan streamResult, 1)
	go func() {
		if stream.Next() {
			firstEvent <- streamResult{event: stream.Current()}
			return
		}
		firstEvent <- streamResult{err: stream.Err()}
	}()

	const prompt = "Create a Python script that generates the first 20 Fibonacci numbers and saves them to fibonacci.txt"
	sent, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
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
		t.Fatalf("send event: %v", err)
	}
	if len(sent.Data) != 1 || sent.Data[0].Type != "user.message" {
		t.Fatalf("unexpected send response: %+v", sent.Data)
	}

	select {
	case result := <-firstEvent:
		if result.err != nil {
			t.Fatalf("stream read: %v", result.err)
		}
		switch event := result.event.AsAny().(type) {
		case anthropic.BetaManagedAgentsUserMessageEvent:
			if len(event.Content) != 1 || event.Content[0].Text != prompt {
				t.Fatalf("unexpected streamed user message: %+v", event.Content)
			}
			t.Logf("Stream observed local user message event before sandbox launch: %s", event.ID)
		case anthropic.BetaManagedAgentsAgentMessageEvent:
			for _, block := range event.Content {
				t.Log(block.Text)
			}
		case anthropic.BetaManagedAgentsAgentToolUseEvent:
			t.Logf("[Using tool: %s]", event.Name)
		case anthropic.BetaManagedAgentsSessionStatusIdleEvent:
			t.Log("Agent finished.")
		default:
			t.Fatalf("unexpected stream event type %q", result.event.Type)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for stream event: %v", ctx.Err())
	}

	quickstartRunRealSandbox(t, ctx, app, environment.ID, session.ID)
}

func quickstartShouldRunRealSandbox(cfg config.Config) bool {
	return strings.TrimSpace(cfg.E2BAPIKey) != "" &&
		!cfg.E2BDebug
}

func quickstartRequireRealSandboxConfig(t *testing.T, cfg config.Config) {
	t.Helper()
	if strings.TrimSpace(cfg.E2BAPIKey) == "" && !cfg.E2BDebug {
		t.Fatal("E2B_API_KEY is required in the current .env for the real quickstart sandbox run")
	}
	if cfg.E2BDebug {
		t.Fatal("E2B_DEBUG must be false for the real quickstart sandbox run")
	}
	if baseURL := quickstartConfiguredSandboxIngressBaseURL(cfg); quickstartLooksLikeLoopbackURL(baseURL) {
		t.Fatalf("code session ingress URL used inside E2B must be reachable from inside E2B, got %q", baseURL)
	}
}

func quickstartSandboxIngressBaseURL(cfg config.Config) string {
	if baseURL := quickstartConfiguredSandboxIngressBaseURL(cfg); baseURL != "" {
		return baseURL
	}
	return quickstartHostDockerBaseURLFromAddr(cfg.Addr)
}

func quickstartConfiguredSandboxIngressBaseURL(cfg config.Config) string {
	for _, value := range []string{cfg.CodeSessionSandboxAPIBaseURL, cfg.CodeSessionAPIBaseURL, cfg.PublicBaseURL} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return strings.TrimRight(trimmed, "/")
		}
	}
	return ""
}

func quickstartHostDockerBaseURLFromAddr(addr string) string {
	port := "8080"
	addr = strings.TrimSpace(addr)
	if addr != "" {
		if _, parsedPort, err := net.SplitHostPort(addr); err == nil && parsedPort != "" {
			port = parsedPort
		} else if strings.HasPrefix(addr, ":") && strings.TrimPrefix(addr, ":") != "" {
			port = strings.TrimPrefix(addr, ":")
		}
	}
	return "http://host.docker.internal:" + port
}

func quickstartHostDockerBaseURLFromServerURL(serverURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid server URL %q", serverURL)
	}
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", fmt.Errorf("server URL %q has no port", serverURL)
		}
	}
	return parsed.Scheme + "://host.docker.internal:" + port, nil
}

func quickstartLooksLikeLoopbackURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "://127.") || strings.Contains(value, "://localhost") || strings.Contains(value, "://[::1]")
}

func quickstartRunRealSandbox(t *testing.T, ctx context.Context, app *testApp, environmentID, sessionID string) {
	t.Helper()
	if app == nil {
		t.Fatal("real quickstart sandbox run requires an in-process test app")
	}
	quickstartEnsureSandboxIngress(t, app)

	workID := quickstartFindSessionEnvironmentWorkID(t, app, environmentID, sessionID)

	provider := e2bruntime.NewProvider(app.cfg)
	var providerSandboxID string
	stopped := false
	defer func() {
		if providerSandboxID != "" && !stopped {
			killCtx, cancelKill := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancelKill()
			_ = provider.Kill(killCtx, providerSandboxID)
		}
	}()

	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, app.cfg, nil, app.credentials)
	processed, err := runner.RunOnce(ctx, "quickstart-real-e2b")
	if err != nil {
		t.Fatalf("run environment runner once: %v", err)
	}
	if !processed {
		t.Fatal("environment runner did not process queued session work")
	}

	codeSessionID, metadata := quickstartWaitForCodeSessionMetadata(t, ctx, app, sessionID)
	if strings.TrimSpace(codeSessionID) == "" || metadata["runtime"] != "claude_code_local" {
		t.Fatalf("session metadata was not patched with local code session ids: %#v", metadata)
	}

	var workState string
	providerSandboxID, workState = quickstartWaitForProviderSandboxMetadata(t, ctx, app, environmentID, workID)
	if workState != "active" && workState != "running" {
		t.Fatalf("environment work state = %s, want active", workState)
	}
	if strings.TrimSpace(providerSandboxID) == "" {
		t.Fatal("provider sandbox id was not recorded")
	}

	sandbox, err := e2b.Connect(ctx, providerSandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: quickstartE2BConnectionOptsFromConfig(app.cfg),
	})
	if err != nil {
		t.Fatalf("connect to real sandbox %s: %v", providerSandboxID, err)
	}

	probe := quickstartWaitForEnvironmentManagerProcess(t, ctx, sandbox, codeSessionID)
	t.Logf("VM sandbox %s started environment-manager for code session %s:\n%s", providerSandboxID, codeSessionID, probe)

	fileProbe := quickstartWaitForFibonacciFile(t, ctx, sandbox, codeSessionID)
	t.Logf("VM sandbox %s completed Fibonacci file request:\n%s", providerSandboxID, fileProbe)
	if transcript, err := quickstartWaitForSessionTranscript(t, ctx, app, sessionID, 45*time.Second); err != nil {
		t.Logf("Claude Code transcript unavailable for %s: %v", codeSessionID, err)
	} else if strings.TrimSpace(transcript) != "" {
		t.Logf("Claude Code transcript for %s:\n%s", codeSessionID, transcript)
	}

	quickstartStopEnvironmentWork(t, ctx, app, environmentID, workID)
	stopped = true
}

func quickstartEnsureSandboxIngress(t *testing.T, app *testApp) {
	t.Helper()
	quickstartRequireRealSandboxConfig(t, app.cfg)
	if quickstartConfiguredSandboxIngressBaseURL(app.cfg) != "" {
		return
	}
	baseURL, err := quickstartHostDockerBaseURLFromServerURL(app.baseURL)
	if err != nil {
		t.Fatalf("derive host.docker.internal ingress URL from test API server %q: %v", app.baseURL, err)
	}
	app.cfg.CodeSessionSandboxAPIBaseURL = baseURL
	t.Logf("Using local sandbox ingress URL %s for in-process API server %s", baseURL, app.baseURL)
}

func quickstartFindSessionEnvironmentWorkID(t *testing.T, app *testApp, environmentID, sessionID string) string {
	t.Helper()
	page := quickstartListEnvironmentWork(t, app, environmentID)
	for _, work := range page.Data {
		var data struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(work.Data, &data); err != nil {
			t.Fatalf("decode work data for %s: %v", work.ID, err)
		}
		if data.Type == "session" && data.ID == sessionID {
			return work.ID
		}
	}
	t.Fatalf("session work for %s not found via environments API: %+v", sessionID, page.Data)
	return ""
}

func quickstartWaitForCodeSessionMetadata(t *testing.T, ctx context.Context, app *testApp, sessionID string) (string, map[string]any) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last map[string]any
	for {
		session := retrieveSession(t, app, sessionID, defaultTestKey)
		metadata := map[string]any{}
		if err := json.Unmarshal(session.Metadata, &metadata); err != nil {
			t.Fatalf("decode session metadata: %v", err)
		}
		last = metadata
		codeSessionID, _ := metadata["claude_code_session_id"].(string)
		if strings.TrimSpace(codeSessionID) != "" && metadata["runtime"] == "claude_code_local" {
			return codeSessionID, metadata
		}
		if time.Now().After(deadline) {
			t.Fatalf("session metadata was not patched with local code session ids: %#v", last)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for session code session metadata: %v; last metadata=%#v", ctx.Err(), last)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func quickstartWaitForProviderSandboxMetadata(t *testing.T, ctx context.Context, app *testApp, environmentID, workID string) (string, string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last environmentWorkAPIResponse
	for {
		work := quickstartRetrieveEnvironmentWork(t, app, environmentID, workID)
		last = work
		metadata := map[string]any{}
		if err := json.Unmarshal(work.Metadata, &metadata); err != nil {
			t.Fatalf("decode work metadata for %s: %v", workID, err)
		}
		providerSandboxID, _ := metadata["provider_sandbox_id"].(string)
		if strings.TrimSpace(providerSandboxID) != "" {
			return providerSandboxID, work.State
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider sandbox id was not exposed via work API; last work=%+v metadata=%s", last, last.Metadata)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for provider sandbox metadata: %v; last work=%+v metadata=%s", ctx.Err(), last, last.Metadata)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func quickstartListEnvironmentWork(t *testing.T, app *testApp, environmentID string) environmentWorkPageAPIResponse {
	t.Helper()
	resp := doEnvironmentRequest(t, app, http.MethodGet, "/v1/environments/"+environmentID+"/work?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list work status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page environmentWorkPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func quickstartRetrieveEnvironmentWork(t *testing.T, app *testApp, environmentID, workID string) environmentWorkAPIResponse {
	t.Helper()
	resp := doEnvironmentRequest(t, app, http.MethodGet, "/v1/environments/"+environmentID+"/work/"+workID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve work status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var work environmentWorkAPIResponse
	decodeJSON(t, resp.Body, &work)
	return work
}

func quickstartWaitForEnvironmentManagerProcess(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, codeSessionID string) string {
	t.Helper()
	logPath := "/tmp/claude-code-sessions/" + quickstartSandboxSafeCodeSessionID(codeSessionID) + "/environment-manager.log"
	command := fmt.Sprintf(`
ps -eo pid=,args=ww | grep -E '[e]nvironment-manager task-run|[/]opt/claude-code/bin/claude' || true
printf '%%s\n' '--- claude command ---'
if [ -f /tmp/claude-command ]; then cat /tmp/claude-command; else printf 'claude command missing\n'; fi
printf '%%s\n' '--- claude-code log ---'
if [ -f /tmp/claude-code.log ]; then
  grep -Ei 'code/sessions|sdk-url|session|remote|transport|worker|epoch|error|warn|failed|websocket|poll|ingress|ccr' /tmp/claude-code.log | tail -n 120
else
  printf 'claude-code log missing\n'
fi
printf '%%s\n' '--- environment-manager log ---'
if [ -f %[1]s ]; then grep -Ei 'sdk url|code session|session|worker|epoch|error|warn|failed|ingress|ccr|claude code' %[1]s | tail -n 120; else printf 'log file missing: %[1]s\n'; fi
`, quickstartShellPath(logPath))

	deadline := time.Now().Add(75 * time.Second)
	var last string
	for {
		stdout, stderr, err := quickstartRunE2BCommand(ctx, sandbox, command, 30*time.Second)
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

func quickstartE2BConnectionOptsFromConfig(cfg config.Config) e2b.ConnectionOpts {
	requestTimeoutMs := int(cfg.E2BRequestTimeout / time.Millisecond)
	debug := cfg.E2BDebug
	return e2b.ConnectionOpts{
		ApiKey:           cfg.E2BAPIKey,
		AccessToken:      cfg.E2BAccessToken,
		Domain:           cfg.E2BDomain,
		ApiUrl:           cfg.E2BAPIURL,
		SandboxUrl:       cfg.E2BSandboxURL,
		Debug:            &debug,
		RequestTimeoutMs: &requestTimeoutMs,
	}
}

func quickstartRunE2BCommand(ctx context.Context, sandbox *e2b.Sandbox, command string, timeout time.Duration) (string, string, error) {
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

func quickstartWaitForFibonacciFile(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, codeSessionID string) string {
	t.Helper()
	logPath := "/tmp/claude-code-sessions/" + quickstartSandboxSafeCodeSessionID(codeSessionID) + "/environment-manager.log"
	command := fmt.Sprintf(`
set +e
file=$(
  for dir in /home/user /workspace /mnt/user-data; do
    if [ -d "$dir" ]; then find "$dir" -maxdepth 5 -type f -name fibonacci.txt 2>/dev/null; fi
  done | head -n 1
)
if [ -n "$file" ]; then
  printf 'fibonacci_file=%%s\n' "$file"
printf '%%s\n' '--- fibonacci.txt ---'
  cat "$file"
fi
printf '%%s\n' '--- processes ---'
ps -eo pid=,args=ww | grep -E '[e]nvironment-manager task-run|[/]opt/claude-code/bin/claude' || true
printf '%%s\n' '--- claude command ---'
if [ -f /tmp/claude-command ]; then cat /tmp/claude-command; else printf 'claude command missing\n'; fi
printf '%%s\n' '--- claude-code log ---'
if [ -f /tmp/claude-code.log ]; then
  grep -Ei 'code/sessions|sdk-url|session|remote|transport|worker|epoch|error|warn|failed|websocket|poll|ingress|ccr' /tmp/claude-code.log | tail -n 160
else
  printf 'claude-code log missing\n'
fi
printf '%%s\n' '--- environment-manager log ---'
if [ -f %[1]s ]; then grep -Ei 'sdk url|code session|session|worker|epoch|error|warn|failed|ingress|ccr|claude code' %[1]s | tail -n 160; else printf 'log file missing: %[1]s\n'; fi
`, quickstartShellPath(logPath))

	deadline := time.Now().Add(4 * time.Minute)
	var last string
	for {
		stdout, stderr, err := quickstartRunE2BCommand(ctx, sandbox, command, 30*time.Second)
		last = strings.TrimSpace(stdout + "\n" + stderr)
		if err == nil && strings.Contains(stdout, "fibonacci_file=") && quickstartContainsFirst20Fibonacci(stdout) {
			return last
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("fibonacci.txt was not created with the expected first 20 Fibonacci numbers; last probe error: %v\n%s", err, last)
			}
			t.Fatalf("fibonacci.txt was not created with the expected first 20 Fibonacci numbers\n%s", last)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for fibonacci.txt: %v\n%s", ctx.Err(), last)
		case <-time.After(5 * time.Second):
		}
	}
}

func quickstartWaitForSessionTranscript(t *testing.T, ctx context.Context, app *testApp, sessionID string, timeout time.Duration) (string, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for {
		transcript, finished, err := quickstartFetchSessionTranscript(t, app, sessionID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(transcript) != "" {
			last = transcript
		}
		if finished || timeout <= 0 || time.Now().After(deadline) {
			return last, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func quickstartFetchSessionTranscript(t *testing.T, app *testApp, sessionID string) (string, bool, error) {
	t.Helper()
	page := listSessionEvents(t, app, sessionID, "order=asc&limit=1000", defaultTestKey)
	events := make([]map[string]any, 0, len(page.Data))
	for _, raw := range page.Data {
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			return "", false, err
		}
		events = append(events, event)
	}
	transcript, finished := quickstartTranscriptFromCodeSessionEvents(events)
	return transcript, finished, nil
}

func quickstartTranscriptFromCodeSessionEvents(events []map[string]any) (string, bool) {
	lines := make([]string, 0, len(events))
	agentFinished := false
	for _, event := range events {
		switch strings.TrimSpace(quickstartStringValue(event["type"])) {
		case "agent.message":
			for _, block := range quickstartContentBlocks(event) {
				switch strings.TrimSpace(quickstartStringValue(block["type"])) {
				case "text":
					if text := strings.TrimSpace(quickstartStringValue(block["text"])); text != "" {
						lines = append(lines, text)
					}
				case "tool_use":
					if name := strings.TrimSpace(quickstartStringValue(block["name"])); name != "" {
						lines = append(lines, "[Using tool: "+strings.ToLower(name)+"]")
					}
				}
			}
		case "session.status_idle":
			agentFinished = true
		}
	}
	if agentFinished {
		lines = append(lines, "Agent finished.")
	}
	return strings.Join(quickstartCompactTranscriptLines(lines), "\n"), agentFinished
}

func quickstartContentBlocks(event map[string]any) []map[string]any {
	if blocks := quickstartMapSlice(event["content"]); len(blocks) > 0 {
		return blocks
	}
	message, _ := event["message"].(map[string]any)
	return quickstartMapSlice(message["content"])
}

func quickstartMapSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		block, ok := item.(map[string]any)
		if ok {
			out = append(out, block)
		}
	}
	return out
}

func quickstartStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func quickstartCompactTranscriptLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == line {
			continue
		}
		out = append(out, line)
	}
	return out
}

func quickstartContainsFirst20Fibonacci(output string) bool {
	matches := regexp.MustCompile(`-?\d+`).FindAllString(output, -1)
	expected := []string{"0", "1", "1", "2", "3", "5", "8", "13", "21", "34", "55", "89", "144", "233", "377", "610", "987", "1597", "2584", "4181"}
	for start := 0; start+len(expected) <= len(matches); start++ {
		ok := true
		for i, want := range expected {
			if matches[start+i] != want {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func quickstartStopEnvironmentWork(t *testing.T, ctx context.Context, app *testApp, environmentID, workID string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, app.baseURL+"/v1/environments/"+environmentID+"/work/"+workID+"/stop?beta=true", strings.NewReader(`{"force":true}`))
	if err != nil {
		t.Fatalf("new stop request: %v", err)
	}
	req.Header.Set("X-Api-Key", defaultTestKey)
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("stop work request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stop work status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func quickstartSandboxSafeCodeSessionID(codeSessionID string) string {
	return strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(codeSessionID)
}

func quickstartShellPath(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

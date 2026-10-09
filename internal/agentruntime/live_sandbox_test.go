package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func TestLiveSandboxMCPDemo(t *testing.T) {
	endpoint := os.Getenv("OMA_DEMO_MCP_URL")
	if endpoint == "" {
		t.Skip("OMA_DEMO_MCP_URL is required for the real sandbox demo")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	connection, err := connectMCP(ctx, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.close()
	path := fmt.Sprintf("/tmp/oma-mcp-demo/demo-%d.txt", time.Now().UnixNano())
	rejected, err := connection.session.CallTool(ctx, demoMCPParams("file_read", map[string]string{"file_path": path}))
	if err != nil || !rejected.IsError {
		t.Fatalf("missing file was not rejected: result=%v error=%v", rejected, err)
	}
	steps := []struct {
		name  string
		input map[string]string
	}{
		{"Write", map[string]string{"file_path": path, "content": "hello from OMA Go host agent\n"}},
		{"Edit", map[string]string{"file_path": path, "old_string": "hello", "new_string": "verified"}},
		{"Grep", map[string]string{"pattern": "verified", "path": path, "output_mode": "content"}},
		{"Glob", map[string]string{"pattern": "demo-*.txt", "path": "/tmp/oma-mcp-demo"}},
		{"Bash", map[string]string{"command": "cat " + path}},
		{"Read", map[string]string{"file_path": path}},
	}
	var responses [][]fantasy.StreamPart
	for index, step := range steps {
		input, err := json.Marshal(step.input)
		if err != nil {
			t.Fatal(err)
		}
		responses = append(responses, []fantasy.StreamPart{
			{Type: fantasy.StreamPartTypeToolCall, ID: fmt.Sprintf("demo_call_%d", index), ToolCallName: step.name, ToolCallInput: string(input)},
			{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
		})
	}
	responses = append(responses, hostTextStep("DEMO_OK"))
	model := &hostExecutionModel{responses: responses}
	fixture := newLiveDemoExecution(t, ctx, cancel, connection, model, launchConfig{})
	worker := fixture.worker
	fixture.deliver("demo_input", "Use the sandbox tools to write, edit, search, list, execute and read the demo file.")
	for _, step := range steps {
		permission := fixture.permission()
		if permission.ToolName != step.name {
			t.Fatalf("tool order: got %s want %s", permission.ToolName, step.name)
		}
		fixture.respond(permission, "allow", permission.Input)
	}
	fixture.finished("demo_input")
	results := 0
	for _, event := range worker.publicEvents() {
		if event.Type == "system.message" {
			t.Fatalf("demo failed: %#v", event)
		}
		if event.Type == "agent.tool_result" {
			results++
			if event.IsError {
				t.Fatalf("sandbox tool failed: %#v", event)
			}
		}
	}
	if results != len(steps) || model.count() != len(steps)+1 {
		t.Fatalf("incomplete loop: results=%d model_calls=%d", results, model.count())
	}
	read, err := connection.session.CallTool(ctx, demoMCPParams("file_read", map[string]string{"file_path": path}))
	if err != nil || read.IsError {
		t.Fatalf("verify sandbox file: result=%v error=%v", read, err)
	}
	encoded, err := json.Marshal(read)
	if err != nil || !strings.Contains(string(encoded), "verified from OMA Go host agent") {
		t.Fatalf("sandbox file content mismatch: %s error=%v", encoded, err)
	}
	final := hostEventOfType(t, worker.publicEvents(), "agent.message")
	if len(final.Content) != 1 || final.Content[0].Text != "DEMO_OK" {
		t.Fatalf("missing final answer: %#v", final)
	}
	t.Logf("DEMO_OK endpoint=%s file=%s tools=%d model_steps=%d; scripted model and in-process Worker, real E2B sandbox MCP", endpoint, path, results, model.count())
}

func TestLiveModelSandboxMCPDemo(t *testing.T) {
	endpoint := os.Getenv("OMA_DEMO_MCP_URL")
	baseURL := os.Getenv("OMA_DEMO_MODEL_BASE_URL")
	modelID := os.Getenv("OMA_DEMO_MODEL_ID")
	apiKey := os.Getenv("OMA_DEMO_MODEL_API_KEY")
	if endpoint == "" || baseURL == "" || modelID == "" || apiKey == "" {
		t.Skip("real MCP endpoint, model base URL, model ID and model API key are required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	connection, err := connectMCP(ctx, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.close()
	provider, err := anthropic.New(anthropic.WithBaseURL(baseURL), anthropic.WithAPIKey(apiKey))
	if err != nil {
		t.Fatal("initialize model provider")
	}
	model, err := provider.LanguageModel(ctx, modelID)
	if err != nil {
		t.Fatal("initialize language model")
	}
	path := fmt.Sprintf("/tmp/oma-mcp-demo/real-model-%d.txt", time.Now().UnixNano())
	configuration := launchConfig{Mode: Host, Model: modelID, SystemPrompt: "Execute the user's sandbox demo with the provided tools. Use each of the six tools. Wait for each tool result before the next step. Do not simulate tool results. Reply exactly DEMO_OK only after verifying the file content."}
	fixture := newLiveDemoExecution(t, ctx, cancel, connection, model, configuration)
	prompt := fmt.Sprintf("Use Write to write 'hello from OMA Go host agent\\n' to %s with file_path and content. Then use Edit to replace hello with verified using old_string and new_string. Use Grep to find verified in that file with output_mode=content. Use Glob to list real-model-*.txt in /tmp/oma-mcp-demo. Use Bash to cat %s. Finally use Read to read %s and confirm 'verified from OMA Go host agent'. Execute these six steps in order, then reply DEMO_OK.", path, path, path)
	fixture.deliver("real_model_input", prompt)
	used := approveLiveDemoTools(t, fixture, "real_model_input")
	for _, name := range []string{"Write", "Edit", "Grep", "Glob", "Bash", "Read"} {
		if !used[name] {
			t.Fatalf("real model did not execute %s", name)
		}
	}
	var final publicEvent
	for _, event := range fixture.worker.publicEvents() {
		if event.Type == "system.message" || event.Type == "agent.tool_result" && event.IsError {
			t.Fatal("real model demo reported an execution error")
		}
		if event.Type == "agent.message" {
			final = event
		}
	}
	read, err := connection.session.CallTool(ctx, demoMCPParams("file_read", map[string]string{"file_path": path}))
	if err != nil || read.IsError {
		t.Fatal("verify real model sandbox file")
	}
	encoded, err := json.Marshal(read)
	if err != nil || !strings.Contains(string(encoded), "verified from OMA Go host agent") {
		t.Fatal("real model sandbox file content mismatch")
	}
	if len(final.Content) != 1 || !strings.Contains(final.Content[0].Text, "DEMO_OK") {
		t.Fatal("real model did not return DEMO_OK")
	}
	t.Logf("DEMO_OK model=%s endpoint=%s file=%s tools=%d; real model and E2B sandbox MCP, in-process Worker", modelID, endpoint, path, len(used))
}

func newLiveDemoExecution(t *testing.T, ctx context.Context, cancel context.CancelFunc, connection *mcpConnection, model fantasy.LanguageModel, configuration launchConfig) *hostExecutionFixture {
	t.Helper()
	worker := &hostExecutionWorker{inputs: make(chan codesessions.HostInput, 16), requested: make(chan codesessions.HostToolRequest, 8), changed: make(chan struct{}), acknowledgements: make(map[string]int), seenHistory: make(map[string]bool)}
	service := &Service{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), active: make(map[string]*execution)}
	executor := &execution{service: service, worker: worker, codeSessionID: "cs_live_demo", epoch: 1, cancel: cancel, model: model, configuration: configuration, connections: []*mcpConnection{connection}, permissions: make(map[string]chan permissionResponse), done: make(chan struct{})}
	service.active[executor.codeSessionID] = executor
	t.Cleanup(func() {
		cancel()
		select {
		case <-executor.done:
		case <-time.After(5 * time.Second):
			t.Error("demo executor failed to stop")
		}
	})
	go executor.run(ctx)
	worker.wait(ctx, t, func(worker *hostExecutionWorker) bool { return worker.state == "idle" })
	return &hostExecutionFixture{t: t, ctx: ctx, worker: worker, executor: executor}
}

func approveLiveDemoTools(t *testing.T, fixture *hostExecutionFixture, inputID string) map[string]bool {
	t.Helper()
	used := make(map[string]bool)
	for calls := 0; ; {
		fixture.worker.mu.Lock()
		finished := false
		for _, entry := range fixture.worker.history {
			if entry.Role == "run_finished" && entry.InputEventID == inputID && fixture.worker.state == "idle" {
				finished = true
			}
		}
		changed := fixture.worker.changed
		fixture.worker.mu.Unlock()
		if finished {
			return used
		}
		select {
		case permission := <-fixture.worker.requested:
			calls++
			if calls > 16 {
				t.Fatal("real model exceeded demo tool call limit")
			}
			used[permission.ToolName] = true
			fixture.respond(permission, "allow", permission.Input)
			t.Logf("approved real model tool: %s", permission.ToolName)
		case <-changed:
		case <-fixture.executor.done:
			t.Fatal("real model executor stopped before completing the demo")
		case <-fixture.ctx.Done():
			t.Fatal("real model demo timed out")
		}
	}
}

func demoMCPParams(name string, arguments map[string]string) *mcp.CallToolParams {
	return &mcp.CallToolParams{Name: name, Arguments: arguments}
}

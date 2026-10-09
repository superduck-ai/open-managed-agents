package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func TestHostExecutionPermissionDenialDoesNotCallSandbox(t *testing.T) {
	fixture := newHostExecutionFixture(t, false, hostToolStep(), hostTextStep("denied"))
	fixture.deliver("input_1", "write the file")
	permission := fixture.permission()
	fixture.respond(permission, "deny", nil)
	fixture.finished("input_1")
	if fixture.sandbox.count() != 0 || fixture.model.count() != 2 {
		t.Fatalf("denial executed tool: sandbox=%d model=%d", fixture.sandbox.count(), fixture.model.count())
	}
	fixture.worker.mu.Lock()
	states := append([]string(nil), fixture.worker.states...)
	fixture.worker.mu.Unlock()
	if len(states) < 5 || states[len(states)-2] != "running" {
		t.Fatalf("denial did not resume model execution: %#v", states)
	}
	events := fixture.worker.publicEvents()
	result := hostEventOfType(t, events, "agent.tool_result")
	if !result.IsError || result.ToolUseID != "public_"+permission.RequestID {
		t.Fatalf("denial result lost its tool association: %#v", result)
	}
}

func TestHostExecutionAssistantHistoryFailurePreventsSandboxDispatch(t *testing.T) {
	fixture := newHostExecutionFixture(t, false, hostToolStep(), hostTextStep("unreachable"))
	fixture.worker.failHistory(fantasy.MessageRoleAssistant)
	fixture.deliver("input_1", "write the file")
	fixture.finished("input_1")
	if fixture.sandbox.count() != 0 || fixture.model.count() != 1 || fixture.worker.permissionCount() != 0 {
		t.Fatalf("continued after failed assistant persistence: sandbox=%d model=%d permissions=%d", fixture.sandbox.count(), fixture.model.count(), fixture.worker.permissionCount())
	}
	_ = hostEventOfType(t, fixture.worker.publicEvents(), "system.message")
}

func TestHostExecutionToolHistoryFailurePreventsNextModelRequest(t *testing.T) {
	fixture := newHostExecutionFixture(t, false, hostToolStep(), hostTextStep("unreachable"))
	fixture.worker.failHistory(fantasy.MessageRoleTool)
	fixture.deliver("input_1", "write the file")
	permission := fixture.permission()
	fixture.respond(permission, "allow", json.RawMessage(`{"path":"approved.txt","content":"approved"}`))
	fixture.finished("input_1")
	if fixture.sandbox.count() != 1 || fixture.model.count() != 1 {
		t.Fatalf("continued after failed result persistence: sandbox=%d model=%d", fixture.sandbox.count(), fixture.model.count())
	}
	for _, event := range fixture.worker.publicEvents() {
		if event.Type == "agent.tool_result" {
			t.Fatal("published a tool result whose private history was not saved")
		}
	}
}

func TestHostExecutionUnknownMCPOutcomeDoesNotRetryOrInventResult(t *testing.T) {
	fixture := newHostExecutionFixture(t, true, hostToolStep(), hostTextStep("unreachable"))
	fixture.deliver("input_1", "write the file")
	permission := fixture.permission()
	fixture.respond(permission, "allow", json.RawMessage(`{"path":"approved.txt","content":"approved"}`))
	fixture.finished("input_1")
	if fixture.sandbox.count() != 1 || fixture.model.count() != 1 {
		t.Fatalf("unknown outcome retried: sandbox=%d model=%d", fixture.sandbox.count(), fixture.model.count())
	}
	for _, event := range fixture.worker.publicEvents() {
		if event.Type == "agent.tool_result" {
			t.Fatal("unknown outcome produced a synthetic tool result")
		}
	}
	fixture.deliver("input_1", "write the file")
	fixture.worker.wait(fixture.ctx, t, func(worker *hostExecutionWorker) bool {
		return worker.acknowledgements["input_1:processed"] >= 2
	})
	if fixture.sandbox.count() != 1 || fixture.model.count() != 1 {
		t.Fatal("input redelivery repeated a call with unknown outcome")
	}
}

func TestHostExecutionApprovalUpdatedInputRedeliveryAndContinuation(t *testing.T) {
	fixture := newHostExecutionFixture(t, false, hostToolStep(), hostTextStep("first answer"), hostTextStep("second answer"))
	fixture.deliver("input_1", "write the file")
	permission := fixture.permission()
	if permission.ToolName != "Write" || permission.ThreadID != "thread_1" {
		t.Fatalf("permission request did not preserve canonical tool/thread: %#v", permission)
	}
	fixture.deliver("input_1", "write the file")
	fixture.worker.wait(fixture.ctx, t, func(worker *hostExecutionWorker) bool {
		return worker.acknowledgements["input_1:processed"] >= 2
	})
	fixture.respond(permission, "allow", json.RawMessage(`{"path":"approved.txt","content":"user changed content"}`))
	fixture.finished("input_1")
	calls := fixture.sandbox.snapshot()
	if len(calls) != 1 || calls[0].Path != "approved.txt" || calls[0].Content != "user changed content" {
		t.Fatalf("MCP did not receive approved arguments: %#v", calls)
	}
	if fixture.worker.permissionCount() != 1 || fixture.model.count() != 2 {
		t.Fatalf("redelivery created duplicate work: permissions=%d model=%d", fixture.worker.permissionCount(), fixture.model.count())
	}
	fixture.deliver("input_2", "continue")
	fixture.finished("input_2")
	prompt := fixture.model.prompt(2)
	var roles []fantasy.MessageRole
	for _, message := range prompt {
		if message.Role != fantasy.MessageRoleSystem {
			roles = append(roles, message.Role)
		}
	}
	wantRoles := []fantasy.MessageRole{fantasy.MessageRoleUser, fantasy.MessageRoleAssistant, fantasy.MessageRoleTool, fantasy.MessageRoleAssistant, fantasy.MessageRoleUser}
	if len(roles) != len(wantRoles) {
		t.Fatalf("second turn did not load complete model history: %#v", roles)
	}
	for index, want := range wantRoles {
		if roles[index] != want {
			t.Fatalf("second turn history role[%d]=%s want %s", index, roles[index], want)
		}
	}
	events := fixture.worker.publicEvents()
	use := hostEventOfType(t, events, "agent.tool_use")
	result := hostEventOfType(t, events, "agent.tool_result")
	if use.ID == "" || result.ToolUseID != use.ID || result.IsError || result.SessionThreadID != "thread_1" {
		t.Fatalf("tool association is invalid: use=%#v result=%#v", use, result)
	}
	starts := make(map[string]bool)
	deltas := make(map[string]bool)
	finals := make(map[string]bool)
	for _, event := range events {
		switch event.Type {
		case "event_start":
			if event.Event != nil && event.Event.Type == "agent.message" {
				starts[event.Event.ID] = true
			}
		case "event_delta":
			deltas[event.EventID] = true
		case "agent.message":
			finals[event.ID] = true
		}
	}
	if len(finals) != 2 || len(starts) != 2 {
		t.Fatalf("unexpected preview/final counts: starts=%#v finals=%#v", starts, finals)
	}
	for id := range finals {
		if !starts[id] || !deltas[id] {
			t.Fatalf("preview and final identities differ for %s", id)
		}
	}
}

func TestHostExecutionInterruptBeforeDispatchAllowsNextTurn(t *testing.T) {
	fixture := newHostExecutionFixture(t, false, hostToolStep(), hostTextStep("next answer"))
	fixture.deliver("input_1", "write the file")
	permission := fixture.permission()
	fixture.worker.wait(fixture.ctx, t, func(worker *hostExecutionWorker) bool { return worker.state == "requires_action" })
	payload := inputPayload{Type: "control_request"}
	payload.Request.Subtype = "interrupt"
	raw, _ := json.Marshal(payload)
	fixture.worker.inputs <- codesessions.HostInput{EventID: "interrupt_1", Payload: raw}
	fixture.finished("input_1")
	if fixture.sandbox.count() != 0 {
		t.Fatal("interrupted approval dispatched a tool")
	}
	result := hostEventOfType(t, fixture.worker.publicEvents(), "agent.tool_result")
	if !result.IsError || result.ToolUseID != "public_"+permission.RequestID {
		t.Fatalf("interrupted result lost association: %#v", result)
	}
	fixture.deliver("input_2", "continue")
	fixture.finished("input_2")
	if fixture.model.count() != 2 {
		t.Fatal("unexecuted tool call blocked the next turn")
	}
}

func TestHostExecutionQueuesAcceptedUserInputWhileAwaitingApproval(t *testing.T) {
	fixture := newHostExecutionFixture(t, false, hostToolStep(), hostTextStep("first answer"), hostTextStep("second answer"))
	fixture.deliver("input_1", "write the file")
	permission := fixture.permission()
	for range 40 {
		fixture.deliver("input_2", "continue")
	}
	fixture.worker.wait(fixture.ctx, t, func(worker *hostExecutionWorker) bool { return worker.acknowledgements["input_2:received"] >= 40 })
	fixture.respond(permission, "allow", json.RawMessage(`{"path":"approved.txt","content":"approved"}`))
	fixture.finished("input_2")
	if fixture.model.count() != 3 || fixture.sandbox.count() != 1 {
		t.Fatalf("queued input lost: model=%d sandbox=%d", fixture.model.count(), fixture.sandbox.count())
	}
}

type hostExecutionFixture struct {
	t        *testing.T
	ctx      context.Context
	worker   *hostExecutionWorker
	model    *hostExecutionModel
	sandbox  *hostSandbox
	executor *execution
}

func newHostExecutionFixture(t *testing.T, unknownOutcome bool, responses ...[]fantasy.StreamPart) *hostExecutionFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	sandbox := &hostSandbox{}
	server := mcp.NewServer(&mcp.Implementation{Name: "host-runtime-test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "file_write", Description: "Write a sandbox file."}, func(_ context.Context, _ *mcp.CallToolRequest, input hostSandboxInput) (*mcp.CallToolResult, any, error) {
		sandbox.mu.Lock()
		sandbox.calls = append(sandbox.calls, input)
		sandbox.mu.Unlock()
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "saved"}}}, nil, nil
	})
	protocolHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || !unknownOutcome {
			protocolHandler.ServeHTTP(writer, request)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		var payload struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(body, &payload) != nil || payload.Method != "tools/call" {
			protocolHandler.ServeHTTP(writer, request)
			return
		}
		protocolHandler.ServeHTTP(httptest.NewRecorder(), request)
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	connection, err := connectMCP(ctx, httpServer.URL, nil)
	if err != nil {
		cancel()
		httpServer.Close()
		t.Fatalf("connect real MCP transport: %v", err)
	}
	worker := &hostExecutionWorker{inputs: make(chan codesessions.HostInput, 16), requested: make(chan codesessions.HostToolRequest, 8), changed: make(chan struct{}), acknowledgements: make(map[string]int), seenHistory: make(map[string]bool)}
	model := &hostExecutionModel{responses: responses}
	service := &Service{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), active: make(map[string]*execution)}
	executor := &execution{service: service, worker: worker, codeSessionID: "cs_test", epoch: 1, cancel: cancel, model: model, connections: []*mcpConnection{connection}, permissions: make(map[string]chan permissionResponse), done: make(chan struct{})}
	service.active[executor.codeSessionID] = executor
	fixture := &hostExecutionFixture{t: t, ctx: ctx, worker: worker, model: model, sandbox: sandbox, executor: executor}
	t.Cleanup(func() {
		cancel()
		select {
		case <-executor.done:
		case <-time.After(5 * time.Second):
			t.Error("host executor failed to stop")
		}
		httpServer.Close()
	})
	go executor.run(ctx)
	worker.wait(ctx, t, func(worker *hostExecutionWorker) bool { return worker.state == "idle" })
	return fixture
}

func (f *hostExecutionFixture) deliver(eventID, prompt string) {
	content, _ := json.Marshal(prompt)
	payload := inputPayload{Type: "user", SessionThreadID: "thread_1"}
	payload.Message.Content = content
	raw, _ := json.Marshal(payload)
	select {
	case f.worker.inputs <- codesessions.HostInput{EventID: eventID, Payload: raw}:
	case <-f.ctx.Done():
		f.t.Fatal("timed out delivering host input")
	}
}

func (f *hostExecutionFixture) permission() codesessions.HostToolRequest {
	select {
	case request := <-f.worker.requested:
		return request
	case <-f.ctx.Done():
		f.t.Fatal("timed out waiting for host tool permission")
		return codesessions.HostToolRequest{}
	}
}

func (f *hostExecutionFixture) respond(permission codesessions.HostToolRequest, behavior string, input json.RawMessage) {
	payload := inputPayload{Type: "control_response"}
	payload.Response.RequestID = permission.RequestID
	payload.Response.Response = permissionResponse{Behavior: behavior, ToolUseID: permission.ToolUseID, UpdatedInput: input}
	raw, _ := json.Marshal(payload)
	select {
	case f.worker.inputs <- codesessions.HostInput{EventID: "confirmation_" + permission.RequestID, Payload: raw}:
	case <-f.ctx.Done():
		f.t.Fatal("timed out delivering tool approval")
	}
}

func (f *hostExecutionFixture) finished(inputEventID string) {
	f.worker.wait(f.ctx, f.t, func(worker *hostExecutionWorker) bool {
		if worker.state != "idle" {
			return false
		}
		for _, entry := range worker.history {
			if entry.InputEventID == inputEventID && entry.Role == "run_finished" {
				return true
			}
		}
		return false
	})
}

type hostSandboxInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type hostSandbox struct {
	mu    sync.Mutex
	calls []hostSandboxInput
}

func (s *hostSandbox) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *hostSandbox) snapshot() []hostSandboxInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]hostSandboxInput(nil), s.calls...)
}

type hostExecutionWorker struct {
	mu               sync.Mutex
	inputs           chan codesessions.HostInput
	requested        chan codesessions.HostToolRequest
	changed          chan struct{}
	history          []codesessions.HostHistoryEntry
	seenHistory      map[string]bool
	events           []publicEvent
	acknowledgements map[string]int
	permissions      int
	state            string
	states           []string
	failRole         fantasy.MessageRole
}

func (w *hostExecutionWorker) failHistory(role fantasy.MessageRole) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failRole = role
}

func (w *hostExecutionWorker) Next(ctx context.Context) (codesessions.HostInput, error) {
	select {
	case input := <-w.inputs:
		return input, nil
	case <-ctx.Done():
		return codesessions.HostInput{}, ctx.Err()
	}
}

func (w *hostExecutionWorker) Acknowledge(_ context.Context, eventID, status string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.acknowledgements[eventID+":"+status]++
	w.notify()
	return nil
}

func (w *hostExecutionWorker) Heartbeat(context.Context) error { return nil }
func (w *hostExecutionWorker) Close(context.Context) error     { return nil }

func (w *hostExecutionWorker) SetState(_ context.Context, state string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = state
	w.states = append(w.states, state)
	w.notify()
	return nil
}

func (w *hostExecutionWorker) EndTurn(ctx context.Context, _ bool) error {
	return w.SetState(ctx, "idle")
}

func (w *hostExecutionWorker) Publish(_ context.Context, payload json.RawMessage) error {
	var event publicEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
	w.notify()
	return nil
}

func (w *hostExecutionWorker) LoadHistory(context.Context) ([]codesessions.HostHistoryEntry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]codesessions.HostHistoryEntry(nil), w.history...), nil
}

func (w *hostExecutionWorker) AppendHistory(_ context.Context, entries []codesessions.HostHistoryEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, entry := range entries {
		if entry.Role == "message" {
			var message fantasy.Message
			if err := json.Unmarshal(entry.Payload, &message); err != nil {
				return err
			}
			if message.Role == w.failRole {
				return errors.New("private history unavailable")
			}
		}
	}
	for _, entry := range entries {
		if !w.seenHistory[entry.ID] {
			w.seenHistory[entry.ID] = true
			w.history = append(w.history, entry)
		}
	}
	w.notify()
	return nil
}

func (w *hostExecutionWorker) RequestPermission(ctx context.Context, request codesessions.HostToolRequest) (codesessions.HostToolPermission, error) {
	w.mu.Lock()
	w.permissions++
	w.events = append(w.events, publicEvent{Type: "agent.tool_use", ID: "public_" + request.RequestID, SessionThreadID: request.ThreadID})
	w.notify()
	w.mu.Unlock()
	select {
	case w.requested <- request:
		return codesessions.HostToolPermission{RequestID: request.RequestID, PublicEventID: "public_" + request.RequestID, Behavior: "ask"}, nil
	case <-ctx.Done():
		return codesessions.HostToolPermission{}, ctx.Err()
	}
}

func (w *hostExecutionWorker) notify() {
	close(w.changed)
	w.changed = make(chan struct{})
}

func (w *hostExecutionWorker) wait(ctx context.Context, t *testing.T, predicate func(*hostExecutionWorker) bool) {
	t.Helper()
	for {
		w.mu.Lock()
		ready := predicate(w)
		changed := w.changed
		w.mu.Unlock()
		if ready {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal("timed out waiting for observable host worker state")
		}
	}
}

func (w *hostExecutionWorker) publicEvents() []publicEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]publicEvent(nil), w.events...)
}

func (w *hostExecutionWorker) permissionCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.permissions
}

type hostExecutionModel struct {
	fantasy.LanguageModel
	mu        sync.Mutex
	calls     []fantasy.Call
	responses [][]fantasy.StreamPart
}

func (*hostExecutionModel) Model() string    { return "host-test" }
func (*hostExecutionModel) Provider() string { return "anthropic" }

func (m *hostExecutionModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	index := len(m.calls)
	m.calls = append(m.calls, call)
	m.mu.Unlock()
	if index >= len(m.responses) {
		return nil, errors.New("unexpected model request")
	}
	return func(yield func(fantasy.StreamPart) bool) {
		for _, part := range m.responses[index] {
			if !yield(part) {
				return
			}
		}
	}, nil
}

func (m *hostExecutionModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *hostExecutionModel) prompt(index int) fantasy.Prompt {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= len(m.calls) {
		return nil
	}
	return m.calls[index].Prompt
}

func hostToolStep() []fantasy.StreamPart {
	return []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeToolCall, ID: "model_call_1", ToolCallName: "Write", ToolCallInput: `{"path":"original.txt","content":"model content"}`},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
	}
}

func hostTextStep(text string) []fantasy.StreamPart {
	return []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeTextStart, ID: "text"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: text},
		{Type: fantasy.StreamPartTypeTextEnd, ID: "text"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop},
	}
}

func hostEventOfType(t *testing.T, events []publicEvent, eventType string) publicEvent {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	t.Fatalf("event %s not published: %#v", eventType, events)
	return publicEvent{}
}

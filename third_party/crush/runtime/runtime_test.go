package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
)

func TestHistoryFailureStopsBeforeModelOrTools(t *testing.T) {
	for _, role := range []fantasy.MessageRole{fantasy.MessageRoleUser, fantasy.MessageRoleAssistant, fantasy.MessageRoleTool} {
		t.Run(string(role), func(t *testing.T) {
			history := &memoryHistory{failRole: role}
			model := &scriptedModel{responses: [][]fantasy.StreamPart{toolStep("call_1"), textStep("done")}}
			var effects atomic.Int32
			tool := countingTool(&effects, nil)
			engine := newTestEngine(t, model, history, &recordingEvents{}, tool)
			_, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "input_1", Prompt: "run tool"})
			if err == nil {
				t.Fatal("expected history failure")
			}
			wantRequests, wantEffects := 1, int32(0)
			if role == fantasy.MessageRoleUser {
				wantRequests = 0
			}
			if role == fantasy.MessageRoleTool {
				wantEffects = 1
			}
			if model.count() != wantRequests || effects.Load() != wantEffects {
				t.Fatalf("requests=%d effects=%d", model.count(), effects.Load())
			}
			if role == fantasy.MessageRoleTool {
				history.failRole = ""
				_, err = engine.Run(context.Background(), Call{SessionID: "session", RunID: "input_2", Prompt: "retry"})
				if !errors.Is(err, ErrPendingTools) || effects.Load() != 1 || model.count() != 1 {
					t.Fatalf("unsafe recovery: %v requests=%d effects=%d", err, model.count(), effects.Load())
				}
			}
		})
	}
}

func TestCallbackTransportErrorNeverRetriesTools(t *testing.T) {
	for _, eventType := range []EventType{EventStepStart, EventToolCall, EventStepResponse, EventToolResult, EventStepFinish, EventRunFinish} {
		t.Run(string(eventType), func(t *testing.T) {
			failure := &net.OpError{Op: "write", Net: "tcp", Err: errors.New("event database unavailable")}
			events := &recordingEvents{failType: eventType, failure: failure}
			model := &scriptedModel{responses: [][]fantasy.StreamPart{toolStep("call_1"), textStep("done")}}
			var effects atomic.Int32
			engine := newTestEngine(t, model, &memoryHistory{}, events, countingTool(&effects, nil))
			_, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "run"})
			if !errors.Is(err, failure) {
				t.Fatalf("callback failure not returned: %v", err)
			}
			wantRequests := 1
			wantEffects := int32(0)
			if eventType == EventStepStart {
				wantRequests = 0
			}
			if eventType == EventToolResult || eventType == EventStepFinish || eventType == EventRunFinish {
				wantEffects = 1
			}
			if eventType == EventRunFinish {
				wantRequests = 2
			}
			if model.count() != wantRequests || effects.Load() != wantEffects {
				t.Fatalf("requests=%d effects=%d", model.count(), effects.Load())
			}
		})
	}
}

func TestUnknownToolOutcomeIsFatalAndLeavesRecoverablePendingCall(t *testing.T) {
	failure := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("MCP disconnected after dispatch")}
	model := &scriptedModel{responses: [][]fantasy.StreamPart{toolStep("call_1"), textStep("should not run")}}
	history := &memoryHistory{}
	events := &recordingEvents{}
	var effects atomic.Int32
	engine := newTestEngine(t, model, history, events, countingTool(&effects, &ExecutionError{Cause: failure}))
	_, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run_1", Prompt: "run"})
	if !errors.Is(err, failure) || effects.Load() != 1 || model.count() != 1 {
		t.Fatalf("err=%v effects=%d requests=%d", err, effects.Load(), model.count())
	}
	for _, event := range events.events {
		if event.Type == EventToolResult || event.Type == EventRunFinish {
			t.Fatalf("fabricated completion: %s", event.Type)
		}
	}
	_, err = engine.Run(context.Background(), Call{SessionID: "session", RunID: "run_2", Prompt: "retry"})
	if !errors.Is(err, ErrPendingTools) || model.count() != 1 {
		t.Fatalf("pending call replayed: %v", err)
	}
}

func TestFailedResultPersistenceStopsRemainingSequentialTools(t *testing.T) {
	history := &memoryHistory{failRole: fantasy.MessageRoleTool}
	step := toolStep("first")
	step = append(step[:1], fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "second", ToolCallName: "sandbox_tool", ToolCallInput: "{}"}, step[1])
	model := &scriptedModel{responses: [][]fantasy.StreamPart{step}}
	var effects atomic.Int32
	engine := newTestEngine(t, model, history, &recordingEvents{}, countingTool(&effects, nil))
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "run"}); err == nil {
		t.Fatal("expected tool result persistence failure")
	}
	if effects.Load() != 1 || model.count() != 1 {
		t.Fatalf("continued after persistence failure: effects=%d requests=%d", effects.Load(), model.count())
	}
}

func TestStreamFailureDoesNotPersistPartialAssistant(t *testing.T) {
	model := &scriptedModel{responses: [][]fantasy.StreamPart{{
		{Type: fantasy.StreamPartTypeTextStart, ID: "block"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "block", Delta: "partial"},
		{Type: fantasy.StreamPartTypeError, Error: errors.New("stream failed")},
	}}}
	history := &memoryHistory{}
	engine := newTestEngine(t, model, history, &recordingEvents{})
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "run"}); err == nil {
		t.Fatal("expected stream failure")
	}
	if len(history.messages) != 1 || history.messages[0].Role != fantasy.MessageRoleUser {
		t.Fatalf("partial response was persisted: %#v", history.messages)
	}
}

func TestCancelAndBusySession(t *testing.T) {
	started := make(chan struct{})
	model := &scriptedModel{started: started}
	engine := newTestEngine(t, model, &memoryHistory{}, &recordingEvents{})
	done := make(chan error, 1)
	go func() {
		_, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run_1", Prompt: "wait"})
		done <- err
	}()
	<-started
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run_2", Prompt: "wait"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected busy: %v", err)
	}
	engine.Cancel("session")
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel did not end stream: %v", err)
	}
}

func TestProviderRetryUsesNewAttemptAndDiscardsPartialText(t *testing.T) {
	model := &scriptedModel{responses: [][]fantasy.StreamPart{{
		{Type: fantasy.StreamPartTypeTextStart, ID: "text"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: "partial"},
		{Type: fantasy.StreamPartTypeError, Error: &fantasy.ProviderError{StatusCode: 503, Message: "temporarily unavailable", ResponseHeaders: map[string]string{"retry-after-ms": "1"}}},
	}, textStep("complete")}}
	events := &recordingEvents{}
	history := &memoryHistory{}
	engine := newTestEngine(t, model, history, events)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := engine.Run(ctx, Call{SessionID: "session", RunID: "run", Prompt: "run"}); err != nil {
		t.Fatal(err)
	}
	var retried, completed bool
	for _, event := range events.events {
		if event.Type == EventRetry {
			retried = event.Attempt == 1
		}
		if event.Type == EventTextEnd {
			completed = event.Attempt == 2 && event.Text == "complete"
		}
	}
	if !retried || !completed || len(history.messages) != 2 || model.count() != 2 {
		t.Fatalf("retry history corrupted: retry=%v complete=%v history=%#v", retried, completed, history.messages)
	}
}

func TestToolHistoryIsDurableBeforeNextModelRequest(t *testing.T) {
	history := &memoryHistory{}
	model := &scriptedModel{responses: [][]fantasy.StreamPart{toolStep("call_1"), textStep("done")}}
	model.before = func(index int, call fantasy.Call) {
		if index == 1 {
			if len(history.messages) != 3 || history.messages[2].Role != fantasy.MessageRoleTool {
				t.Fatalf("next model request ran before durable result: %#v", history.messages)
			}
			if len(call.Prompt) != 3 {
				t.Fatalf("next model request lacks tool history: %#v", call.Prompt)
			}
		}
	}
	var effects atomic.Int32
	engine := newTestEngine(t, model, history, &recordingEvents{}, countingTool(&effects, nil))
	result, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "run"})
	if err != nil || len(result.Steps) != 2 || effects.Load() != 1 || len(history.messages) != 4 {
		t.Fatalf("result=%#v err=%v effects=%d history=%d", result, err, effects.Load(), len(history.messages))
	}
}

func TestRestoredHistoryAndPerCallTools(t *testing.T) {
	history := &memoryHistory{}
	model := &scriptedModel{responses: [][]fantasy.StreamPart{textStep("previous answer"), toolStep("call_1"), textStep("done")}}
	engine := newTestEngine(t, model, history, &recordingEvents{})
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run_1", Prompt: "first"}); err != nil {
		t.Fatal(err)
	}
	engine = newTestEngine(t, model, history, &recordingEvents{})
	var effects atomic.Int32
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run_2", Prompt: "second", Tools: []fantasy.AgentTool{countingTool(&effects, nil)}}); err != nil {
		t.Fatal(err)
	}
	if effects.Load() != 1 || len(model.calls[1].Prompt) != 3 || len(history.messages) != 6 {
		t.Fatalf("history not restored: %#v", model.calls)
	}
}

func TestBoundedLoopAndOrdinaryToolErrors(t *testing.T) {
	model := &scriptedModel{responses: [][]fantasy.StreamPart{toolStep("call_1"), toolStep("call_2"), textStep("unreachable")}}
	history := &memoryHistory{}
	events := &recordingEvents{}
	var effects atomic.Int32
	engine, err := New(Options{Model: model, History: history, Events: events, MaxSteps: 2, Tools: []fantasy.AgentTool{countingTool(&effects, errors.New("ordinary tool failure"))}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "run"})
	if err != nil || len(result.Steps) != 2 || effects.Load() != 2 || model.count() != 2 || hasPendingTools(history.messages) {
		t.Fatalf("bounded loop failed: result=%#v err=%v", result, err)
	}
}

func TestReasoningSignatureSurvivesHistorySerialization(t *testing.T) {
	fantasy.RegisterProviderType("runtime.signature", func(data []byte) (fantasy.ProviderOptionsData, error) {
		value := &signedReasoning{}
		return value, json.Unmarshal(data, value)
	})
	metadata := fantasy.ProviderMetadata{"test": &signedReasoning{Signature: "private-signature"}}
	model := &scriptedModel{responses: [][]fantasy.StreamPart{{
		{Type: fantasy.StreamPartTypeReasoningStart, ID: "thinking"},
		{Type: fantasy.StreamPartTypeReasoningDelta, ID: "thinking", Delta: "plan"},
		{Type: fantasy.StreamPartTypeReasoningEnd, ID: "thinking", ProviderMetadata: metadata},
		{Type: fantasy.StreamPartTypeTextStart, ID: "text"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: "done"},
		{Type: fantasy.StreamPartTypeTextEnd, ID: "text"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop},
	}, textStep("second answer")}}
	history := &memoryHistory{}
	engine := newTestEngine(t, model, history, &recordingEvents{})
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run_1", Prompt: "first"}); err != nil {
		t.Fatal(err)
	}
	engine = newTestEngine(t, model, history, &recordingEvents{})
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run_2", Prompt: "second"}); err != nil {
		t.Fatal(err)
	}
	reasoning, ok := fantasy.AsMessagePart[fantasy.ReasoningPart](model.calls[1].Prompt[1].Content[0])
	if !ok {
		t.Fatal("missing restored thinking")
	}
	signature, ok := reasoning.ProviderOptions["test"].(*signedReasoning)
	if !ok || signature.Signature != "private-signature" {
		t.Fatalf("signature not restored: %#v", reasoning)
	}
}

func TestTextAndReasoningEventsCarryStableIDs(t *testing.T) {
	model := &scriptedModel{responses: [][]fantasy.StreamPart{{
		{Type: fantasy.StreamPartTypeReasoningStart, ID: "reasoning", Delta: "plan"},
		{Type: fantasy.StreamPartTypeReasoningDelta, ID: "reasoning", Delta: " now"},
		{Type: fantasy.StreamPartTypeReasoningEnd, ID: "reasoning"},
		{Type: fantasy.StreamPartTypeTextStart, ID: "text"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: "done"},
		{Type: fantasy.StreamPartTypeTextEnd, ID: "text"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop},
	}}}
	events := &recordingEvents{}
	history := &memoryHistory{}
	engine := newTestEngine(t, model, history, events)
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "run"}); err != nil {
		t.Fatal(err)
	}
	var sawResponse bool
	for _, event := range events.events {
		if event.SessionID != "session" || event.RunID != "run" || event.Step != 0 || event.Attempt != 1 {
			t.Fatalf("missing event scope: %#v", event)
		}
		if event.Type == EventReasoningEnd && event.Text != "plan now" {
			t.Fatalf("reasoning lost: %#v", event)
		}
		if event.Type == EventStepResponse {
			sawResponse = true
			if len(event.BlockIDs) != 2 || event.BlockIDs[0] != "reasoning" || event.BlockIDs[1] != "text" {
				t.Fatalf("block IDs changed: %#v", event)
			}
		}
	}
	if !sawResponse || len(history.messages[1].Content) != 2 {
		t.Fatal("missing response history")
	}
}

func newTestEngine(t *testing.T, model fantasy.LanguageModel, history History, events EventSink, tools ...fantasy.AgentTool) *Engine {
	t.Helper()
	engine, err := New(Options{Model: model, History: history, Events: events, Tools: tools, MaxRetries: 2})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

type memoryHistory struct {
	mu       sync.Mutex
	messages []fantasy.Message
	failRole fantasy.MessageRole
}

func (h *memoryHistory) Load(context.Context, string) ([]fantasy.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]fantasy.Message(nil), h.messages...), nil
}

func (h *memoryHistory) Append(_ context.Context, _ string, messages []fantasy.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, message := range messages {
		if message.Role == h.failRole {
			return errors.New("history unavailable")
		}
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	var persisted []fantasy.Message
	if err := json.Unmarshal(data, &persisted); err != nil {
		return err
	}
	h.messages = append(h.messages, persisted...)
	return nil
}

type recordingEvents struct {
	mu       sync.Mutex
	events   []Event
	failType EventType
	failure  error
}

func (e *recordingEvents) Publish(_ context.Context, event Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if event.Type == e.failType {
		return e.failure
	}
	e.events = append(e.events, event)
	return nil
}

type scriptedModel struct {
	fantasy.LanguageModel
	mu        sync.Mutex
	calls     []fantasy.Call
	responses [][]fantasy.StreamPart
	before    func(int, fantasy.Call)
	started   chan struct{}
}

func (*scriptedModel) Model() string    { return "runtime-test" }
func (*scriptedModel) Provider() string { return "anthropic" }

func (m *scriptedModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	index := len(m.calls)
	m.calls = append(m.calls, call)
	m.mu.Unlock()
	if m.before != nil {
		m.before(index, call)
	}
	if m.started != nil {
		close(m.started)
		return func(yield func(fantasy.StreamPart) bool) {
			<-ctx.Done()
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: ctx.Err()})
		}, nil
	}
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

func (m *scriptedModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func countingTool(effects *atomic.Int32, failure error) fantasy.AgentTool {
	return fantasy.NewAgentTool("sandbox_tool", "test sandbox tool", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		effects.Add(1)
		return fantasy.NewTextResponse("tool result"), failure
	})
}

func toolStep(id string) []fantasy.StreamPart {
	return []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeToolCall, ID: id, ToolCallName: "sandbox_tool", ToolCallInput: "{}"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
	}
}

func textStep(text string) []fantasy.StreamPart {
	return []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeTextStart, ID: "text"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: text},
		{Type: fantasy.StreamPartTypeTextEnd, ID: "text"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop},
	}
}

type signedReasoning struct {
	Signature string `json:"signature"`
}

func (*signedReasoning) Options() {}

func (s signedReasoning) MarshalJSON() ([]byte, error) {
	type plain signedReasoning
	return fantasy.MarshalProviderType("runtime.signature", plain(s))
}

func (s *signedReasoning) UnmarshalJSON(data []byte) error {
	type plain signedReasoning
	var value plain
	if err := fantasy.UnmarshalProviderType(data, &value); err != nil {
		return err
	}
	*s = signedReasoning(value)
	return nil
}

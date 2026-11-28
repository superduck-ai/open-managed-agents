package fantasyadapter

import (
	"context"
	"errors"
	"slices"
	"testing"

	"charm.land/fantasy"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

func TestCollectFantasyStreamRejectsTruncatedToolTurn(t *testing.T) {
	model := &stepTestModel{parts: []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeToolInputStart, ID: "toolu_test", ToolCallName: "add"},
		{Type: fantasy.StreamPartTypeToolInputDelta, ID: "toolu_test", Delta: `{"number":`},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonLength},
	}}
	message, err := NewModel(model, nil).Complete(t.Context(), []harness.Message{harness.UserMessage{Text: "Add two numbers."}})
	if err == nil || err.Error() != `unexpected model stream finish reason "length"` || message.Message.Call.ID != "" {
		t.Fatalf("got call %#v and error %v for truncated turn", message.Message.Call, err)
	}
}

func TestFantasyStreamRejectsMultipleToolCalls(t *testing.T) {
	model := &stepTestModel{parts: []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeToolCall, ID: "one", ToolCallName: "add", ToolCallInput: "{}"},
		{Type: fantasy.StreamPartTypeToolCall, ID: "two", ToolCallName: "add", ToolCallInput: "{}"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
	}}
	result, err := NewModel(model, nil).Complete(t.Context(), []harness.Message{harness.UserMessage{Text: "Call add."}})
	if err == nil || err.Error() != "harness supports one client tool call per completion" || result.Message.Call.ID != "" {
		t.Fatalf("multiple tool calls = %+v, %v", result.Message.Call, err)
	}
}

func TestFantasyStreamPropagatesCallbackError(t *testing.T) {
	model := &stepTestModel{parts: []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: "first"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: "second"},
	}}
	callbackError := errors.New("could not publish text")
	callbacks := StreamCallbacks{OnTextDelta: func(_ string, delta string) error {
		if delta != "first" {
			t.Errorf("unexpected delta after callback error: %q", delta)
		}
		return callbackError
	}}
	_, err := NewModel(model, nil).WithStreamCallbacks(callbacks).Complete(t.Context(), []harness.Message{harness.UserMessage{Text: "Reply."}})
	if !errors.Is(err, callbackError) {
		t.Fatalf("completion error = %v", err)
	}
}

type stepTestModel struct {
	fantasy.LanguageModel
	parts []fantasy.StreamPart
	calls []fantasy.Call
}

func (m *stepTestModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls = append(m.calls, call)
	return slices.Values(m.parts), nil
}

func TestFantasyStreamYieldsToolForHarnessScheduling(t *testing.T) {
	model := &stepTestModel{parts: []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeToolInputStart, ID: "toolu_test", ToolCallName: "add"},
		{Type: fantasy.StreamPartTypeToolInputDelta, ID: "toolu_test", Delta: `{"number":`},
		{Type: fantasy.StreamPartTypeToolInputDelta, ID: "toolu_test", Delta: "2}"},
		{Type: fantasy.StreamPartTypeToolInputEnd, ID: "toolu_test"},
		{Type: fantasy.StreamPartTypeToolCall, ID: "toolu_test", ToolCallName: "add", ToolCallInput: `{ "number": 2 }`},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
	}}
	definition := fantasy.FunctionTool{Name: "add"}
	completion, err := NewModel(model, []fantasy.Tool{definition}).Complete(t.Context(), []harness.Message{harness.UserMessage{Text: "Add two numbers."}})
	if err != nil || len(model.calls) != 1 || len(model.calls[0].Tools) != 1 ||
		completion.Message.Call != (harness.ToolCall{ID: "toolu_test", Name: "add", Arguments: `{"number":2}`}) {
		t.Fatalf("model did not return the tool for external dispatch: %+v, %v", completion, err)
	}
}

func TestFantasyStreamUsesFinalToolArgumentsWithoutDeltas(t *testing.T) {
	model := &stepTestModel{parts: []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeToolInputStart, ID: "toolu_test", ToolCallName: "add"},
		{Type: fantasy.StreamPartTypeToolInputEnd, ID: "toolu_test"},
		{Type: fantasy.StreamPartTypeToolCall, ID: "toolu_test", ToolCallName: "add", ToolCallInput: "{}"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
	}}
	result, err := NewModel(model, nil).Complete(t.Context(), []harness.Message{harness.UserMessage{Text: "Call add."}})
	if err != nil || result.Message.Call.Arguments != "{}" {
		t.Fatalf("tool call with no input deltas = %+v, %v", result.Message.Call, err)
	}
}

func TestFantasyStreamPreservesTextAndReasoningCallbacks(t *testing.T) {
	model := &stepTestModel{parts: []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeReasoningStart, ID: "reasoning", Delta: "start"},
		{Type: fantasy.StreamPartTypeReasoningDelta, ID: "reasoning", Delta: " end"},
		{Type: fantasy.StreamPartTypeReasoningEnd, ID: "reasoning"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: "one\n\n"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: "two"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop, Usage: fantasy.Usage{OutputTokens: 5}},
	}}
	var events []string
	callbacks := StreamCallbacks{
		OnReasoningStart: func(id string, content fantasy.ReasoningContent) error {
			events = append(events, id+" started with "+content.Text)
			return nil
		},
		OnReasoningEnd: func(id string, content fantasy.ReasoningContent) error {
			events = append(events, id+" ended with "+content.Text)
			return nil
		},
		OnTextDelta: func(id, delta string) error {
			events = append(events, id+" delta "+delta)
			return nil
		},
	}
	result, err := NewModel(model, nil).WithStreamCallbacks(callbacks).Complete(t.Context(), []harness.Message{harness.UserMessage{Text: "Reply."}})
	if err != nil || len(model.calls) != 1 || result.Message.Text != "one\n\ntwo" || result.Usage == nil || result.Usage.OutputTokens != 5 {
		t.Fatalf("completion = %+v, %v", result, err)
	}
	want := []string{"reasoning started with start", "reasoning ended with start end", "text delta one\n\n", "text delta two"}
	if !slices.Equal(events, want) {
		t.Fatalf("callback events = %q, want %q", events, want)
	}
}

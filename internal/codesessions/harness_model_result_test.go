package codesessions

import (
	"errors"
	"testing"
	"time"

	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestHarnessModelFinishRejectsMissingAction(t *testing.T) {
	frame := HarnessModelFrame{Type: HarnessModelFinish, Result: &HarnessModelFinal{
		Facts:  HarnessModelFacts{EndedAt: time.Now()},
		Blocks: []HarnessModelBlock{{Text: "hello"}},
	}}
	if err := (&Service{}).applyHarnessModelFrame(t.Context(), &ModelRequest{}, frame); !errors.Is(err, ErrProtocol) {
		t.Fatalf("missing action = %v, want protocol error", err)
	}
}

func TestHarnessModelResultKeepsOnlyRestorableText(t *testing.T) {
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test"}
	redacted := ModelRequestMessage{ID: "redacted", Type: "agent.message", Content: []ModelRequestContent{{Type: "redacted"}}}
	nontext := harnessTextResult(request, ModelRequestResult{Messages: []ModelRequestMessage{redacted}})
	if nontext.ErrorType != "invalid_response" || len(nontext.Messages) != 0 {
		t.Fatalf("non-text generation was accepted: %+v", nontext)
	}
	message := ModelRequestMessage{ID: "reply", Type: "agent.message", Content: []ModelRequestContent{{Type: "text", Text: new("hello")}}}
	result := harnessTextResult(request, ModelRequestResult{Messages: []ModelRequestMessage{redacted, message}})
	if result.ErrorType != "" || len(result.Messages) != 1 || len(result.Messages[0].Content) != 1 ||
		len(result.EventIDs) != 1 || result.EventIDs[0] != request.harnessMessageID() || result.Messages[0].Content[0].Text == nil ||
		*result.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("unexpected harness reply: %+v", result)
	}
}

func TestHarnessModelResultRejectsAmbiguousToolCall(t *testing.T) {
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test"}
	for _, input := range []ModelRequestResult{
		{ToolUseIDs: []string{"one"}, ToolUses: []ModelRequestToolUse{{ID: "two", Name: "Read", Input: []byte(`{}`)}}},
		{ToolUseIDs: []string{"one", "two"}, ToolUses: []ModelRequestToolUse{{ID: "one"}, {ID: "two"}}},
	} {
		result := harnessTextResult(request, input)
		if result.ErrorType != "invalid_response" || len(result.ToolUses) != 0 {
			t.Fatalf("ambiguous call was accepted: %+v", result)
		}
	}
}

func TestHarnessModelResultKeepsToolOnlyCompletion(t *testing.T) {
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test"}
	result := harnessTextResult(request, ModelRequestResult{
		ToolUseIDs:  []string{"one"},
		ToolUses:    []ModelRequestToolUse{{ID: "one", Name: "Read", Input: []byte(`{"file_path":"/tmp/example"}`)}},
		harnessNext: HarnessModelTool,
	})
	if result.ErrorType != "" || len(result.ToolUses) != 1 || len(result.Messages) != 0 {
		t.Fatalf("unexpected tool-only result: %+v", result)
	}
	want := harness.ToolWork{Call: harness.ToolCall{ID: "one", Name: "Read", Arguments: `{"file_path":"/tmp/example"}`}}
	transition, err := harnessModelTransition(request, result)
	if err != nil || transition.Pending != want {
		t.Fatalf("tool-only transition = %+v, %v", transition, err)
	}
}

func TestHarnessModelTransitionWaitsForRootChoice(t *testing.T) {
	call := ModelRequestToolUse{ID: "pick", Name: sessioncontract.HarnessAskUserQuestion,
		Input: []byte(`{"questions":[{"id":"color","question":"Which color?","options":[{"label":"Blue"},{"label":"Green"}]}]}`)}
	request := &ModelRequest{}
	result := harnessTextResult(request, ModelRequestResult{
		ToolUseIDs: []string{call.ID}, ToolUses: []ModelRequestToolUse{call}, harnessNext: HarnessModelUserChoice,
	})
	transition, err := harnessModelTransition(request, result)
	want := harness.UserChoiceWork{Call: harness.ToolCall{ID: harness.ToolCallID(call.ID), Name: call.Name, Arguments: string(call.Input)}}
	if result.ErrorType != "" || err != nil || transition.Pending != want {
		t.Fatalf("choice transition = %+v, %v, result error type = %q", transition, err, result.ErrorType)
	}
}

func TestHarnessModelTransitionRejectsDispatchOfUserChoice(t *testing.T) {
	result := ModelRequestResult{
		harnessNext: HarnessModelTool,
		ToolUses:    []ModelRequestToolUse{{ID: "pick", Name: sessioncontract.HarnessAskUserQuestion}},
	}
	if _, err := harnessModelTransition(&ModelRequest{}, result); !errors.Is(err, ErrProtocol) {
		t.Fatalf("choice dispatched as a tool: %v", err)
	}
}

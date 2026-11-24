package cloudharness

import (
	"errors"
	"reflect"
	"testing"
)

func TestStepRejectsStaleApproval(t *testing.T) {
	state := State{Pending: ApprovalWork{Call: ToolCall{ID: "current", Name: "add"}}}
	_, err := Step(state, Approval{CallID: "stale", Granted: true})
	if !errors.Is(err, ErrUnexpectedInput) {
		t.Fatalf("got %v, want ErrUnexpectedInput", err)
	}
}

func TestStepCanBeReplayed(t *testing.T) {
	state := State{Version: 2, MessageCount: 2, Pending: ModelWork{}}
	call := ToolCall{ID: "current", Name: "add", Arguments: "{}"}
	input := ModelCompleted{Message: AssistantMessage{Call: call}}
	first, err := Step(state, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Step(state, input)
	if err != nil {
		t.Fatal(err)
	}
	want := Transition{Pending: ApprovalWork{Call: call}, Messages: []Message{input.Message}}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(first, second) {
		t.Fatalf("first = %#v, second = %#v, want %#v", first, second, want)
	}
}

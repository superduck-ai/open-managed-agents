package cloudharness

import (
	"reflect"
	"testing"
)

func TestModelTransitionCanBeReplayed(t *testing.T) {
	work := ModelWork{}
	call := ToolCall{ID: "current", Name: "add", Arguments: "{}"}
	message := AssistantMessage{Call: call}
	first, err := modelTransition(work, message)
	if err != nil {
		t.Fatal(err)
	}
	second, err := modelTransition(work, message)
	if err != nil {
		t.Fatal(err)
	}
	want := Transition{Pending: ApprovalWork{Call: call}, Messages: []Message{message}}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(first, second) {
		t.Fatalf("first = %#v, second = %#v, want %#v", first, second, want)
	}
}

func TestModelTransitionAfterCompletion(t *testing.T) {
	tool := ToolCall{ID: "call", Name: "read", Arguments: "{}"}
	choice := ToolCall{ID: "choice", Name: "ask", Arguments: "{}"}
	for _, tt := range []struct {
		name    string
		work    ModelWork
		message AssistantMessage
		pending Work
	}{
		{"text completes the turn", ModelWork{AutoExecuteTools: true}, AssistantMessage{Text: "done"}, nil},
		{"tool runs automatically", ModelWork{AutoExecuteTools: true, UserChoiceTool: choice.Name}, AssistantMessage{Call: tool}, ToolWork{Call: tool}},
		{"choice waits for an answer", ModelWork{AutoExecuteTools: true, UserChoiceTool: choice.Name}, AssistantMessage{Call: choice}, UserChoiceWork{Call: choice}},
		{"choice routing must be enabled", ModelWork{AutoExecuteTools: true}, AssistantMessage{Call: choice}, ToolWork{Call: choice}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := modelTransition(tt.work, tt.message)
			want := Transition{Pending: tt.pending, Messages: []Message{tt.message}}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("transition = %#v, %v, want %#v", got, err, want)
			}
		})
	}
}

package cloudharness

import (
	"errors"
	"fmt"
)

var ErrUnexpectedInput = errors.New("input does not match the pending work")

type Message interface {
	conversationMessage()
}

type Input interface {
	stepInput()
}

type UserMessage struct{ Text string }

type ChoiceResponse struct{}

type ToolResultReady struct{}

type ResumeThread struct{}

type AssistantMessage struct {
	Text string
	Call ToolCall
}

type ToolCall struct {
	ID        ToolCallID `json:"id"`
	Name      string     `json:"name"`
	Arguments string     `json:"arguments"`
}

type ToolResult struct {
	CallID ToolCallID `json:"call_id"`
	Text   string     `json:"text"`
	Failed bool       `json:"failed"`
}

type ModelCompleted struct {
	Message       AssistantMessage
	Usage         *Usage
	ContextWindow uint
	Persisted     bool
}

type toolExecuted struct{ Result ToolResult }

type Approval struct {
	CallID  ToolCallID
	Granted bool
}

func (UserMessage) conversationMessage()      {}
func (AssistantMessage) conversationMessage() {}
func (ToolResult) conversationMessage()       {}

func (ChoiceResponse) stepInput()  {}
func (ToolResultReady) stepInput() {}

func (UserMessage) stepInput()    {}
func (ResumeThread) stepInput()   {}
func (ModelCompleted) stepInput() {}
func (Approval) stepInput()       {}
func (ToolResult) stepInput()     {}
func (toolExecuted) stepInput()   {}

type Work interface {
	pendingWork()
}

type ModelWork struct{}
type ApprovalWork struct{ Call ToolCall }
type ToolWork struct {
	Call   ToolCall
	Result ToolResult
}

func (ModelWork) pendingWork()    {}
func (ApprovalWork) pendingWork() {}
func (ToolWork) pendingWork()     {}

type State struct {
	Version      uint64
	MessageCount int64
	Pending      Work
}

type Transition struct {
	Pending  Work
	Messages []Message
}

func Step(state State, input Input) (Transition, error) {
	switch event := input.(type) {
	case UserMessage:
		if event.Text != "" {
			return afterUserMessage(state.Pending, event), nil
		}
	case ModelCompleted:
		if _, waiting := state.Pending.(ModelWork); waiting {
			return afterModel(event.Message)
		}
	case Approval:
		if work, waiting := state.Pending.(ApprovalWork); waiting && event.CallID == work.Call.ID {
			return afterApproval(work.Call, event.Granted), nil
		}
	case ToolResult:
		if work, waiting := state.Pending.(ToolWork); waiting && event.CallID == work.Call.ID {
			return Transition{Pending: ModelWork{}, Messages: []Message{event}}, nil
		}
	case toolExecuted:
		if work, waiting := state.Pending.(ToolWork); waiting && work.Result.CallID == "" && event.Result.CallID == work.Call.ID {
			work.Result = event.Result
			return Transition{Pending: work}, nil
		}
	}
	return Transition{}, fmt.Errorf("%w: %T while waiting on %T", ErrUnexpectedInput, input, state.Pending)
}

func afterUserMessage(pending Work, message UserMessage) Transition {
	var callID ToolCallID
	switch work := pending.(type) {
	case ApprovalWork:
		callID = work.Call.ID
	case ToolWork:
		callID = work.Call.ID
	}
	var messages []Message
	if callID != "" {
		messages = append(messages, ToolResult{CallID: callID, Text: "The user interrupted this call.", Failed: true})
	}
	return Transition{Pending: ModelWork{}, Messages: append(messages, message)}
}

func afterModel(message AssistantMessage) (Transition, error) {
	transition := Transition{Messages: []Message{message}}
	if message.Call.ID != "" && message.Call.Name != "" {
		transition.Pending = ApprovalWork{Call: message.Call}
	} else if message.Call != (ToolCall{}) || message.Text == "" {
		return Transition{}, fmt.Errorf("%w: incomplete model response", ErrUnexpectedInput)
	}
	return transition, nil
}

func afterApproval(call ToolCall, granted bool) Transition {
	if granted {
		return Transition{Pending: ToolWork{Call: call}}
	}
	result := ToolResult{CallID: call.ID, Text: "The user denied this call.", Failed: true}
	return Transition{Pending: ModelWork{}, Messages: []Message{result}}
}

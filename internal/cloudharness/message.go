package cloudharness

import "encoding/json"

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

type Approval struct {
	CallID  ToolCallID
	Granted bool
}

func (UserMessage) conversationMessage()      {}
func (AssistantMessage) conversationMessage() {}
func (ToolResult) conversationMessage()       {}

func (ChoiceResponse) stepInput()  {}
func (ToolResultReady) stepInput() {}

func (UserMessage) stepInput()  {}
func (ResumeThread) stepInput() {}
func (Approval) stepInput()     {}
func (ToolResult) stepInput()   {}

type InputMessage struct {
	GroupID   ThreadID
	Content   Input
	Metadata  json.RawMessage
	Synthetic bool
}

package cloudharness

import (
	"context"
	"encoding/json"
)

type InputMessage struct {
	GroupID   ThreadID
	Content   Input
	Metadata  json.RawMessage
	Synthetic bool
}

type StepInput struct {
	Message InputMessage
	DedupID string
}

type InputSender interface {
	SendToInputQueue(ctx context.Context, message InputMessage, dedupID string) error
}

type DeferQueue interface {
	Push(ctx context.Context, input InputMessage, messageID string) (bool, error)
	Drain(ctx context.Context, threadID ThreadID) ([]StepInput, error)
}

type ThreadObserver interface {
	OnEvent(threadID ThreadID, event AgentEvent)
}

type AgentEventKind uint8

const (
	CompletionStarted AgentEventKind = iota
	CompletionFinished
	ToolApprovalRequested
	ToolExecutionStarted
)

type AgentEvent struct {
	Kind  AgentEventKind
	Call  ToolCall
	Usage *Usage
}

type Usage struct {
	InputTokens       int64
	OutputTokens      int64
	TotalTokens       int64
	CachedInputTokens int64
}

type StepOutcome interface {
	stepOutcome()
}

type Skipped struct{}

type Completed struct {
	Usage         *Usage
	ContextWindow uint
}

func (Skipped) stepOutcome()   {}
func (Completed) stepOutcome() {}

type ThreadStepOutcome struct {
	ThreadID ThreadID
	Outcome  StepOutcome
}

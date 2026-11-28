package cloudharness

import "context"

type Model interface {
	Complete(context.Context, []Message) (ModelCompleted, error)
}

type ModelCompleted struct {
	Message       AssistantMessage
	Usage         *Usage
	ContextWindow uint
}

type Usage struct {
	InputTokens       int64
	OutputTokens      int64
	TotalTokens       int64
	CachedInputTokens int64
}

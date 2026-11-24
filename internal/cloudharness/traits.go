package cloudharness

import (
	"context"
	"encoding/json"
)

type ThreadID string
type ToolCallID string
type ChoiceID string

type MessageWithID struct {
	Message Message
	ID      string
}

type SpawnContext struct {
	InheritUpTo *int64
}

type ThreadParentInfo struct {
	ThreadID   ThreadID   `json:"thread_id"`
	ToolCallID ToolCallID `json:"tool_call_id"`
}

type Ancestor struct {
	ThreadID  ThreadID
	UpToOrder int64
}

type CompactionSummary struct {
	Summary   string `json:"summary"`
	UpToOrder int64  `json:"up_to_order"`
}

type UserChoice struct {
	ID          ChoiceID `json:"id"`
	Prompt      string   `json:"prompt"`
	Choices     []string `json:"choices"`
	Default     uint     `json:"default"`
	ResponseURL string   `json:"response_url"`
}

type ConversationStore interface {
	EnsureRootThread(ctx context.Context, threadID ThreadID) error
	ThreadExists(ctx context.Context, threadID ThreadID) (bool, error)
	LoadHistoryUpTo(ctx context.Context, threadID ThreadID, startFrom, upTo *int64) ([]Message, error)
	LoadHistoryWithAncestors(ctx context.Context, threadID ThreadID) (history []Message, compactedUpTo *int64, ancestorPrefixLen uint, err error)
	AppendMessages(ctx context.Context, threadID ThreadID, messages []MessageWithID) error
	SpawnThread(ctx context.Context, parentThreadID ThreadID, spawnToolCallID ToolCallID, isForSubscriptionEvent bool, spawnContext SpawnContext) (ThreadID, error)
	IsThreadClosed(ctx context.Context, threadID ThreadID) (bool, error)
	CloseThread(ctx context.Context, threadID ThreadID) error
	IsSubscriptionEventThread(ctx context.Context, threadID ThreadID) (bool, error)
	IsFreshContextThread(ctx context.Context, threadID ThreadID) (bool, error)
	GetThreadParentInfo(ctx context.Context, threadID ThreadID) (*ThreadParentInfo, error)
	GetAncestorChain(ctx context.Context, threadID ThreadID) ([]Ancestor, error)
	MarkThreadAsCompaction(ctx context.Context, threadID ThreadID) error
	IsCompactionThread(ctx context.Context, threadID ThreadID) (bool, error)
	GetThreadSpawnOrder(ctx context.Context, threadID ThreadID) (*int64, error)
	SaveCompactionSummary(ctx context.Context, threadID ThreadID, summary string, upToOrder int64) error
	LoadLatestCompactionSummaryUpTo(ctx context.Context, threadID ThreadID, upToOrder *int64) (*CompactionSummary, error)
}

type StateStore interface {
	GetMetadata(ctx context.Context, rootThreadID ThreadID) (json.RawMessage, error)
	SetMetadata(ctx context.Context, rootThreadID ThreadID, metadata json.RawMessage) error
	GetActiveSubscriptions(ctx context.Context, threadID ThreadID) ([]ToolCallID, error)
	AddActiveSubscription(ctx context.Context, threadID ThreadID, toolCallID ToolCallID) error
	RemoveActiveSubscription(ctx context.Context, threadID ThreadID, toolCallID ToolCallID) error
	AddPendingUserChoice(ctx context.Context, threadID ThreadID, choice UserChoice) error
	RemovePendingUserChoice(ctx context.Context, threadID ThreadID, choiceID ChoiceID) error
	GetPendingUserChoices(ctx context.Context, threadID ThreadID) ([]UserChoice, error)
	IsThreadStopped(ctx context.Context, threadID ThreadID) (bool, error)
}

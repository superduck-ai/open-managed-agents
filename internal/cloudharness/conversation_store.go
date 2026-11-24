package cloudharness

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
)

var ErrInvalidHistoryOrder = errors.New("history bounds must be nonnegative")

func (s *MemoryStore) EnsureRootThread(_ context.Context, threadID ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	if thread.exists {
		return nil
	}
	thread.exists = true
	s.threads[threadID] = thread
	return nil
}

func (s *MemoryStore) ThreadExists(_ context.Context, threadID ThreadID) (bool, error) {
	return s.readThread(threadID).exists, nil
}

func (s *MemoryStore) LoadHistoryUpTo(_ context.Context, threadID ThreadID, startFrom, upTo *int64) ([]Message, error) {
	if (startFrom != nil && *startFrom < 0) || (upTo != nil && *upTo < 0) {
		return nil, ErrInvalidHistoryOrder
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return historyRange(s.threads[threadID].messages, startFrom, upTo), nil
}

func historyRange(messages []MessageWithID, startFrom, upTo *int64) []Message {
	end := int64(len(messages))
	if upTo != nil {
		end = min(end, *upTo)
	}
	var start int64
	if startFrom != nil {
		start = min(end, *startFrom)
	}
	result := make([]Message, 0, end-start)
	for _, message := range messages[start:end] {
		result = append(result, message.Message)
	}
	return result
}

func (s *MemoryStore) LoadHistoryWithAncestors(_ context.Context, threadID ThreadID) ([]Message, *int64, uint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	thread := s.threads[threadID]
	if summary := latestCompactionSummary(thread.summaries, nil); summary != nil {
		history := []Message{summaryMessage(summary.Summary)}
		return append(history, historyRange(thread.messages, &summary.UpToOrder, nil)...), &summary.UpToOrder, 0, nil
	}
	history := s.ancestorHistory(threadID)
	prefixLen := uint(len(history))
	return append(history, historyRange(thread.messages, nil, nil)...), nil, prefixLen, nil
}

func (s *MemoryStore) ancestorHistory(threadID ThreadID) []Message {
	if s.threads[threadID].fresh {
		return nil
	}
	ancestors := s.ancestorChain(threadID)
	for index := len(ancestors) - 1; index >= 0; index-- {
		if s.threads[ancestors[index].ThreadID].fresh {
			ancestors = ancestors[index:]
			break
		}
	}
	var history []Message
	var startFrom *int64
	for index := len(ancestors) - 1; index >= 0; index-- {
		ancestor := ancestors[index]
		summary := latestCompactionSummary(s.threads[ancestor.ThreadID].summaries, &ancestor.UpToOrder)
		if summary != nil {
			history = append(history, summaryMessage(summary.Summary))
			startFrom = &summary.UpToOrder
			ancestors = ancestors[index:]
			break
		}
	}
	for _, ancestor := range ancestors {
		history = append(history, historyRange(s.threads[ancestor.ThreadID].messages, startFrom, &ancestor.UpToOrder)...)
		startFrom = nil
	}
	return history
}

func summaryMessage(summary string) AssistantMessage {
	return AssistantMessage{Text: "[Compacted conversation summary]\n" + summary}
}

func (s *MemoryStore) AppendMessages(_ context.Context, threadID ThreadID, messages []MessageWithID) error {
	if len(messages) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	thread.messages = append(thread.messages, messages...)
	thread.state.Version++
	thread.state.MessageCount = int64(len(thread.messages))
	s.threads[threadID] = thread
	return nil
}

func (s *MemoryStore) SpawnThread(_ context.Context, parentThreadID ThreadID, spawnToolCallID ToolCallID, isForSubscriptionEvent bool, spawnContext SpawnContext) (ThreadID, error) {
	thread := memoryThread{
		exists:       true,
		parent:       &ThreadParentInfo{ThreadID: parentThreadID, ToolCallID: spawnToolCallID},
		subscription: isForSubscriptionEvent,
		fresh:        spawnContext.InheritUpTo == nil,
	}
	if !thread.fresh {
		thread.spawnOrder = *spawnContext.InheritUpTo
		if thread.spawnOrder < 0 {
			return "", ErrInvalidHistoryOrder
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := ThreadID(rand.Text())
	s.threads[id] = thread
	return id, nil
}

func (s *MemoryStore) IsThreadClosed(_ context.Context, threadID ThreadID) (bool, error) {
	return s.readThread(threadID).closed, nil
}

func (s *MemoryStore) CloseThread(_ context.Context, threadID ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if thread := s.threads[threadID]; thread.exists {
		thread.closed = true
		s.threads[threadID] = thread
	}
	return nil
}

func (s *MemoryStore) IsSubscriptionEventThread(_ context.Context, threadID ThreadID) (bool, error) {
	return s.readThread(threadID).subscription, nil
}

func (s *MemoryStore) IsFreshContextThread(_ context.Context, threadID ThreadID) (bool, error) {
	return s.readThread(threadID).fresh, nil
}

func (s *MemoryStore) GetThreadParentInfo(_ context.Context, threadID ThreadID) (*ThreadParentInfo, error) {
	if parent := s.readThread(threadID).parent; parent != nil {
		return new(*parent), nil
	}
	return nil, nil
}

func (s *MemoryStore) GetAncestorChain(_ context.Context, threadID ThreadID) ([]Ancestor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ancestorChain(threadID), nil
}

func (s *MemoryStore) ancestorChain(threadID ThreadID) []Ancestor {
	var ancestors []Ancestor
	for thread := s.threads[threadID]; thread.parent != nil; thread = s.threads[thread.parent.ThreadID] {
		ancestors = append(ancestors, Ancestor{ThreadID: thread.parent.ThreadID, UpToOrder: thread.spawnOrder})
	}
	slices.Reverse(ancestors)
	return ancestors
}

func (s *MemoryStore) MarkThreadAsCompaction(_ context.Context, threadID ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if thread := s.threads[threadID]; thread.exists {
		thread.compaction = true
		s.threads[threadID] = thread
	}
	return nil
}

func (s *MemoryStore) IsCompactionThread(_ context.Context, threadID ThreadID) (bool, error) {
	return s.readThread(threadID).compaction, nil
}

func (s *MemoryStore) GetThreadSpawnOrder(_ context.Context, threadID ThreadID) (*int64, error) {
	thread := s.readThread(threadID)
	if thread.parent == nil {
		return nil, nil
	}
	return &thread.spawnOrder, nil
}

func (s *MemoryStore) SaveCompactionSummary(_ context.Context, threadID ThreadID, summary string, upToOrder int64) error {
	if upToOrder < 0 {
		return ErrInvalidHistoryOrder
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	thread.summaries = append(thread.summaries, CompactionSummary{Summary: summary, UpToOrder: upToOrder})
	s.threads[threadID] = thread
	return nil
}

func (s *MemoryStore) LoadLatestCompactionSummaryUpTo(_ context.Context, threadID ThreadID, upToOrder *int64) (*CompactionSummary, error) {
	if upToOrder != nil && *upToOrder < 0 {
		return nil, ErrInvalidHistoryOrder
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return latestCompactionSummary(s.threads[threadID].summaries, upToOrder), nil
}

func latestCompactionSummary(summaries []CompactionSummary, upToOrder *int64) *CompactionSummary {
	for _, summary := range slices.Backward(summaries) {
		if upToOrder == nil || summary.UpToOrder <= *upToOrder {
			return &summary
		}
	}
	return nil
}

func (s *MemoryStore) readThread(threadID ThreadID) memoryThread {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.threads[threadID]
}

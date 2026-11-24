package db

import (
	"context"

	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

func (s *HarnessConversationStore) LoadHistoryWithAncestors(ctx context.Context, threadID harness.ThreadID) ([]harness.Message, *int64, uint, error) {
	summary, err := s.LoadLatestCompactionSummaryUpTo(ctx, threadID, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	if summary != nil {
		remaining, err := s.LoadHistoryUpTo(ctx, threadID, &summary.UpToOrder, nil)
		return append([]harness.Message{harnessSummaryMessage(summary.Summary)}, remaining...), &summary.UpToOrder, 0, err
	}
	history, err := s.loadAncestorHistory(ctx, threadID)
	if err != nil {
		return nil, nil, 0, err
	}
	prefix := uint(len(history))
	leaf, err := s.LoadHistoryUpTo(ctx, threadID, nil, nil)
	return append(history, leaf...), nil, prefix, err
}

func (s *HarnessConversationStore) loadAncestorHistory(ctx context.Context, threadID harness.ThreadID) ([]harness.Message, error) {
	fresh, err := s.IsFreshContextThread(ctx, threadID)
	if err != nil || fresh {
		return nil, err
	}
	ancestors, err := s.GetAncestorChain(ctx, threadID)
	if err != nil {
		return nil, err
	}
	ancestors, err = s.inheritedHarnessAncestors(ctx, ancestors)
	if err != nil {
		return nil, err
	}
	history, start, remaining, err := s.ancestorCompaction(ctx, ancestors)
	if err != nil {
		return nil, err
	}
	for _, ancestor := range remaining {
		messages, err := s.LoadHistoryUpTo(ctx, ancestor.ThreadID, start, &ancestor.UpToOrder)
		if err != nil {
			return nil, err
		}
		history = append(history, messages...)
		start = nil
	}
	return history, nil
}

func (s *HarnessConversationStore) inheritedHarnessAncestors(ctx context.Context, ancestors []harness.Ancestor) ([]harness.Ancestor, error) {
	for i := len(ancestors) - 1; i >= 0; i-- {
		fresh, err := s.IsFreshContextThread(ctx, ancestors[i].ThreadID)
		if err != nil {
			return nil, err
		}
		if fresh {
			return ancestors[i:], nil
		}
	}
	return ancestors, nil
}

func (s *HarnessConversationStore) ancestorCompaction(ctx context.Context, ancestors []harness.Ancestor) ([]harness.Message, *int64, []harness.Ancestor, error) {
	for i := len(ancestors) - 1; i >= 0; i-- {
		ancestor := ancestors[i]
		summary, err := s.LoadLatestCompactionSummaryUpTo(ctx, ancestor.ThreadID, &ancestor.UpToOrder)
		if err != nil {
			return nil, nil, nil, err
		}
		if summary != nil {
			return []harness.Message{harnessSummaryMessage(summary.Summary)}, &summary.UpToOrder, ancestors[i:], nil
		}
	}
	return nil, nil, ancestors, nil
}

func harnessSummaryMessage(summary string) harness.AssistantMessage {
	return harness.AssistantMessage{Text: "[Compacted conversation summary]\n" + summary}
}

package cloudharness

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
)

type memoryState struct {
	Metadata       json.RawMessage
	Subscriptions  map[ToolCallID]struct{}
	PendingChoices []UserChoice
}

func (s *MemoryStore) GetMetadata(_ context.Context, rootThreadID ThreadID) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.threads[rootThreadID].values.Metadata), nil
}

func (s *MemoryStore) SetMetadata(_ context.Context, rootThreadID ThreadID, metadata json.RawMessage) error {
	if !json.Valid(metadata) {
		return errors.New("metadata must contain valid JSON")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[rootThreadID]
	thread.values.Metadata = slices.Clone(metadata)
	s.threads[rootThreadID] = thread
	return nil
}

func (s *MemoryStore) GetActiveSubscriptions(_ context.Context, threadID ThreadID) ([]ToolCallID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Sorted(maps.Keys(s.threads[threadID].values.Subscriptions)), nil
}

func (s *MemoryStore) AddActiveSubscription(_ context.Context, threadID ThreadID, toolCallID ToolCallID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	subscriptions := maps.Collect(maps.All(thread.values.Subscriptions))
	subscriptions[toolCallID] = struct{}{}
	thread.values.Subscriptions = subscriptions
	s.threads[threadID] = thread
	return nil
}

func (s *MemoryStore) RemoveActiveSubscription(_ context.Context, threadID ThreadID, toolCallID ToolCallID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	thread.values.Subscriptions = maps.Clone(thread.values.Subscriptions)
	delete(thread.values.Subscriptions, toolCallID)
	s.threads[threadID] = thread
	return nil
}

func (s *MemoryStore) AddPendingUserChoice(_ context.Context, threadID ThreadID, choice UserChoice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	choices := slices.Clone(thread.values.PendingChoices)
	choice.Choices = slices.Clone(choice.Choices)
	index := slices.IndexFunc(choices, func(existing UserChoice) bool { return existing.ID == choice.ID })
	if index >= 0 {
		choices[index] = choice
	} else {
		choices = append(choices, choice)
	}
	thread.values.PendingChoices = choices
	s.threads[threadID] = thread
	return nil
}

func (s *MemoryStore) RemovePendingUserChoice(_ context.Context, threadID ThreadID, choiceID ChoiceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	choices := slices.Clone(thread.values.PendingChoices)
	thread.values.PendingChoices = slices.DeleteFunc(choices, func(choice UserChoice) bool { return choice.ID == choiceID })
	s.threads[threadID] = thread
	return nil
}

func (s *MemoryStore) GetPendingUserChoices(_ context.Context, threadID ThreadID) ([]UserChoice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	choices := slices.Clone(s.threads[threadID].values.PendingChoices)
	for index := range choices {
		choices[index].Choices = slices.Clone(choices[index].Choices)
	}
	return choices, nil
}

func (s *MemoryStore) IsThreadStopped(context.Context, ThreadID) (bool, error) {
	return false, nil
}

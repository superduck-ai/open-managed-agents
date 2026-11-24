package cloudharness

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

var ErrVersionConflict = errors.New("thread state version changed")

type ExecutionStateStore interface {
	StateStore
	LoadState(ctx context.Context, threadID ThreadID) (State, error)
	HasAppliedInput(ctx context.Context, threadID ThreadID, dedupID string) (bool, error)
	Commit(ctx context.Context, threadID ThreadID, version uint64, transition Transition, dedupID string) (State, error)
}

type MemoryStore struct {
	mu      sync.RWMutex
	threads map[ThreadID]memoryThread
}

type memoryThread struct {
	state        State
	messages     []MessageWithID
	exists       bool
	parent       *ThreadParentInfo
	spawnOrder   int64
	closed       bool
	subscription bool
	fresh        bool
	compaction   bool
	summaries    []CompactionSummary
	values       memoryState
	stateOnlyIDs map[string]struct{}
}

var _ ExecutionStateStore = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{threads: make(map[ThreadID]memoryThread)}
}

func (s *MemoryStore) LoadState(_ context.Context, threadID ThreadID) (State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.threads[threadID].state, nil
}

func (s *MemoryStore) HasAppliedInput(_ context.Context, threadID ThreadID, dedupID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.threads[threadID].hasAppliedInput(dedupID), nil
}

func (thread memoryThread) hasAppliedInput(dedupID string) bool {
	if dedupID == "" {
		return false
	}
	_, applied := thread.stateOnlyIDs[dedupID]
	return applied || slices.ContainsFunc(thread.messages, func(message MessageWithID) bool { return message.ID == dedupID })
}

func (s *MemoryStore) Commit(_ context.Context, threadID ThreadID, version uint64, transition Transition, dedupID string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threads[threadID]
	if thread.hasAppliedInput(dedupID) {
		return thread.state, nil
	}
	if thread.state.Version != version {
		return State{}, ErrVersionConflict
	}
	for index, message := range transition.Messages {
		id := fmt.Sprintf("%s:%d:%d", threadID, version+1, index)
		if index == len(transition.Messages)-1 && dedupID != "" {
			id = dedupID
		}
		thread.messages = append(thread.messages, MessageWithID{Message: message, ID: id})
	}
	thread.state = State{Version: version + 1, MessageCount: int64(len(thread.messages)), Pending: transition.Pending}
	if dedupID != "" && len(transition.Messages) == 0 {
		if thread.stateOnlyIDs == nil {
			thread.stateOnlyIDs = make(map[string]struct{})
		}
		thread.stateOnlyIDs[dedupID] = struct{}{}
	}
	s.threads[threadID] = thread
	return thread.state, nil
}

package db

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/yourbatis"
)

type HarnessStateStore struct {
	harnessStore
}

var _ harness.StateStore = (*HarnessStateStore)(nil)

func (d *DB) HarnessState(lease HarnessTurnLease) *HarnessStateStore {
	return &HarnessStateStore{harnessStore: harnessStore{database: d, lease: lease}}
}

type harnessThreadState struct {
	MessageCount  int64
	Version       uint64
	Metadata      json.RawMessage
	Subscriptions map[harness.ToolCallID]struct{}
	Choices       []harness.UserChoice
}

func appendHarnessTransition(ctx context.Context, executor yourbatis.Executor, key harnessThreadKey, turnUUID *string, state harnessThreadState, messages []harness.MessageWithID) error {
	count, err := appendHarnessMessages(ctx, NewHarnessConversationMapper(executor), key, turnUUID, state.MessageCount, messages)
	if err != nil || count == state.MessageCount {
		return err
	}
	state.Version++
	state.MessageCount = count
	return saveHarnessTransition(ctx, executor, key, state)
}

func lockHarnessThreadState(ctx context.Context, executor yourbatis.Executor, key harnessThreadKey) (harnessThreadState, error) {
	if _, err := NewHarnessConversationMapper(executor).LockThread(ctx, key); err != nil {
		return harnessThreadState{}, mapNoRows(err)
	}
	return loadHarnessThreadState(ctx, NewHarnessStateMapper(executor), key)
}

func loadHarnessThreadState(ctx context.Context, mapper HarnessStateMapper, key harnessThreadKey) (harnessThreadState, error) {
	row, found, err := mapper.FindState(ctx, key)
	if err != nil {
		return harnessThreadState{}, err
	}
	if !found {
		return harnessThreadState{}, nil
	}
	state := harnessThreadState{Version: row.Version, MessageCount: row.MessageCount}
	if row.Metadata != nil {
		state.Metadata = json.RawMessage(*row.Metadata)
	}
	for _, field := range []struct {
		raw  json.RawMessage
		dest any
	}{
		{row.ActiveSubscriptions, &state.Subscriptions},
		{row.PendingUserChoices, &state.Choices},
	} {
		if err := json.Unmarshal(field.raw, field.dest); err != nil {
			return harnessThreadState{}, err
		}
	}
	return state, nil
}

func saveHarnessState(ctx context.Context, mapper HarnessStateMapper, key harnessThreadKey, state harnessThreadState) error {
	row := harnessStateRow{Version: state.Version}
	if len(state.Metadata) != 0 {
		row.Metadata = new(string(state.Metadata))
	}
	if state.Subscriptions == nil {
		state.Subscriptions = map[harness.ToolCallID]struct{}{}
	}
	if state.Choices == nil {
		state.Choices = []harness.UserChoice{}
	}
	for _, field := range []struct {
		value any
		dest  *json.RawMessage
	}{
		{state.Subscriptions, &row.ActiveSubscriptions},
		{state.Choices, &row.PendingUserChoices},
	} {
		raw, err := json.Marshal(field.value)
		if err != nil {
			return err
		}
		*field.dest = raw
	}
	return mapper.SaveState(ctx, key, row)
}

func saveHarnessTransition(ctx context.Context, executor yourbatis.Executor, key harnessThreadKey, state harnessThreadState) error {
	if err := NewHarnessConversationMapper(executor).UpdateMessageCount(ctx, key, state.MessageCount); err != nil {
		return err
	}
	return saveHarnessState(ctx, NewHarnessStateMapper(executor), key, state)
}

func (s *HarnessStateStore) readState(ctx context.Context, threadID harness.ThreadID) (harnessThreadState, error) {
	return loadHarnessThreadState(ctx, NewHarnessStateMapper(s.database.mapperDB), s.key(threadID))
}

func (s *HarnessStateStore) updateState(ctx context.Context, threadID harness.ThreadID, mutate func(*harnessThreadState)) error {
	return s.transaction(ctx, func(executor yourbatis.Executor) error {
		key := s.key(threadID)
		mapper := NewHarnessConversationMapper(executor)
		if _, err := mapper.EnsureRoot(ctx, key); err != nil {
			return err
		}
		state, err := lockHarnessThreadState(ctx, executor, key)
		if err != nil {
			return err
		}
		mutate(&state)
		return saveHarnessState(ctx, NewHarnessStateMapper(executor), key, state)
	})
}

func (s *HarnessStateStore) GetMetadata(ctx context.Context, rootThreadID harness.ThreadID) (json.RawMessage, error) {
	state, err := s.readState(ctx, rootThreadID)
	if err != nil {
		return nil, err
	}
	return state.Metadata, nil
}

func (s *HarnessStateStore) SetMetadata(ctx context.Context, rootThreadID harness.ThreadID, metadata json.RawMessage) error {
	if !json.Valid(metadata) {
		return errors.New("metadata must contain valid JSON")
	}
	return s.updateState(ctx, rootThreadID, func(state *harnessThreadState) { state.Metadata = metadata })
}

func (s *HarnessStateStore) GetActiveSubscriptions(ctx context.Context, threadID harness.ThreadID) ([]harness.ToolCallID, error) {
	state, err := s.readState(ctx, threadID)
	if err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(state.Subscriptions)), nil
}

func (s *HarnessStateStore) AddActiveSubscription(ctx context.Context, threadID harness.ThreadID, toolCallID harness.ToolCallID) error {
	return s.updateState(ctx, threadID, func(state *harnessThreadState) {
		if state.Subscriptions == nil {
			state.Subscriptions = make(map[harness.ToolCallID]struct{})
		}
		state.Subscriptions[toolCallID] = struct{}{}
	})
}

func (s *HarnessStateStore) RemoveActiveSubscription(ctx context.Context, threadID harness.ThreadID, toolCallID harness.ToolCallID) error {
	return s.updateState(ctx, threadID, func(state *harnessThreadState) { delete(state.Subscriptions, toolCallID) })
}

func (s *HarnessStateStore) AddPendingUserChoice(ctx context.Context, threadID harness.ThreadID, choice harness.UserChoice) error {
	return s.updateState(ctx, threadID, func(state *harnessThreadState) {
		index := slices.IndexFunc(state.Choices, func(current harness.UserChoice) bool { return current.ID == choice.ID })
		if index < 0 {
			state.Choices = append(state.Choices, choice)
			return
		}
		state.Choices[index] = choice
	})
}

func (s *HarnessStateStore) RemovePendingUserChoice(ctx context.Context, threadID harness.ThreadID, choiceID harness.ChoiceID) error {
	return s.updateState(ctx, threadID, func(state *harnessThreadState) {
		state.Choices = slices.DeleteFunc(state.Choices, func(choice harness.UserChoice) bool { return choice.ID == choiceID })
	})
}

func (s *HarnessStateStore) GetPendingUserChoices(ctx context.Context, threadID harness.ThreadID) ([]harness.UserChoice, error) {
	state, err := s.readState(ctx, threadID)
	if err != nil {
		return nil, err
	}
	return state.Choices, nil
}

func (s *HarnessStateStore) IsThreadStopped(context.Context, harness.ThreadID) (bool, error) {
	return false, nil
}

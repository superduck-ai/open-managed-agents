package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/yourbatis"
)

type HarnessConversationStore struct {
	harnessStore
}

type harnessStore struct {
	database *DB
	lease    HarnessTurnLease
}

var _ harness.ConversationStore = (*HarnessConversationStore)(nil)

func (d *DB) HarnessConversation(lease HarnessTurnLease) *HarnessConversationStore {
	return &HarnessConversationStore{harnessStore: harnessStore{database: d, lease: lease}}
}

func (r HarnessTurnRef) conversationScope() harnessConversationScope {
	return harnessConversationScope{OrganizationUUID: r.OrganizationUUID, WorkspaceUUID: r.WorkspaceUUID, SessionUUID: r.SessionUUID}
}

func (s *harnessStore) key(threadID harness.ThreadID) harnessThreadKey {
	return harnessThreadKey{Scope: s.lease.Ref.conversationScope(), UUID: string(threadID)}
}

func (s *harnessStore) turnUUID() *string {
	if s.lease.Epoch == 0 {
		return nil
	}
	if s.lease.DelegationToolUUID != "" {
		return &s.lease.DelegationToolUUID
	}
	return &s.lease.Ref.UUID
}

func (s *harnessStore) mapper() HarnessConversationMapper {
	return NewHarnessConversationMapper(s.database.mapperDB)
}

func (s *harnessStore) transaction(ctx context.Context, apply func(yourbatis.Executor) error) error {
	return s.database.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if s.lease.Epoch != 0 {
			if err := lockHarnessConversationLease(ctx, NewCloudHarnessMapper(executor), s.lease); err != nil {
				return err
			}
		}
		return apply(executor)
	})
}

func (s *HarnessConversationStore) write(ctx context.Context, apply func(HarnessConversationMapper) error) error {
	return s.transaction(ctx, func(executor yourbatis.Executor) error {
		return apply(NewHarnessConversationMapper(executor))
	})
}

func lockHarnessConversationLease(ctx context.Context, mapper CloudHarnessMapper, lease HarnessTurnLease) error {
	if _, err := mapper.LockModelLease(ctx, lease); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkerEpochMismatch
		}
		return err
	}
	row, found, err := mapper.Lock(ctx, lease.Ref)
	if err != nil {
		return err
	}
	if !found || row.State != "running" || row.WorkerEpoch != lease.Epoch {
		return ErrWorkerEpochMismatch
	}
	return nil
}

func (s *HarnessConversationStore) EnsureRootThread(ctx context.Context, threadID harness.ThreadID) error {
	return s.write(ctx, func(mapper HarnessConversationMapper) error {
		_, err := mapper.EnsureRoot(ctx, s.key(threadID))
		return err
	})
}

func (s *HarnessConversationStore) ThreadExists(ctx context.Context, threadID harness.ThreadID) (bool, error) {
	_, found, err := s.mapper().FindThread(ctx, s.key(threadID))
	return found, err
}

func (s *HarnessConversationStore) HasMessageID(ctx context.Context, threadID harness.ThreadID, messageID string) (bool, error) {
	return s.mapper().HasMessageID(ctx, s.key(threadID), messageID)
}

func (s *HarnessConversationStore) LoadHistoryUpTo(ctx context.Context, threadID harness.ThreadID, startFrom, upTo *int64) ([]harness.Message, error) {
	if (startFrom != nil && *startFrom < 0) || (upTo != nil && *upTo < 0) {
		return nil, harness.ErrInvalidHistoryOrder
	}
	records, err := s.mapper().LoadMessages(ctx, s.key(threadID), startFrom, upTo)
	if err != nil {
		return nil, err
	}
	messages := make([]harness.Message, 0, len(records))
	for _, record := range records {
		message, err := decodeHarnessMessage(record)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (s *HarnessConversationStore) AppendMessages(ctx context.Context, threadID harness.ThreadID, messages []harness.MessageWithID) error {
	if len(messages) == 0 {
		return nil
	}
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
		return appendHarnessTransition(ctx, executor, key, s.turnUUID(), state, messages)
	})
}

func appendHarnessMessages(ctx context.Context, mapper HarnessConversationMapper, key harnessThreadKey, turnUUID *string, order int64, messages []harness.MessageWithID) (int64, error) {
	for _, message := range messages {
		data, err := encodeHarnessMessage(message.Message)
		if err != nil {
			return order, err
		}
		if message.ID == "" {
			return order, errors.New("harness conversation message ID is required")
		}
		written, err := mapper.AppendMessages(ctx, key, turnUUID, message.ID, data, order+1)
		if err != nil {
			return order, err
		}
		order += written
	}
	return order, nil
}

func (s *HarnessConversationStore) SpawnThread(ctx context.Context, parentThreadID harness.ThreadID, callID harness.ToolCallID, spawn harness.SpawnContext) (harness.ThreadID, error) {
	if spawn.InheritUpTo != nil && *spawn.InheritUpTo < 0 {
		return "", harness.ErrInvalidHistoryOrder
	}
	child := harness.ThreadID(uuid.NewString())
	err := s.write(ctx, func(mapper HarnessConversationMapper) error {
		return insertSpawnedHarnessThread(ctx, mapper, s.key(child), string(parentThreadID), callID, spawn)
	})
	if err != nil {
		return "", err
	}
	return child, nil
}

func insertSpawnedHarnessThread(ctx context.Context, mapper HarnessConversationMapper, child harnessThreadKey, parentUUID string, callID harness.ToolCallID, spawn harness.SpawnContext) error {
	parent, err := mapper.LockThread(ctx, harnessThreadKey{Scope: child.Scope, UUID: parentUUID})
	if err != nil {
		return mapNoRows(err)
	}
	return mapper.InsertThread(ctx, child, harnessConversationThread{
		RootThreadUUID: parent.RootThreadUUID, ParentThreadUUID: &parent.ThreadUUID,
		SpawnMessageSeq: new(harnessSpawnOrder(spawn)), SpawnToolCallID: new(string(callID)),
		Fresh: spawn.InheritUpTo == nil,
	})
}

func harnessSpawnOrder(spawn harness.SpawnContext) int64 {
	if spawn.InheritUpTo == nil {
		return 0
	}
	return *spawn.InheritUpTo
}

func (s *harnessStore) thread(ctx context.Context, threadID harness.ThreadID) (harnessConversationThread, error) {
	row, _, err := s.mapper().FindThread(ctx, s.key(threadID))
	return row, err
}

func (s *HarnessConversationStore) IsThreadClosed(ctx context.Context, threadID harness.ThreadID) (bool, error) {
	row, err := s.thread(ctx, threadID)
	return row.Closed, err
}

func (s *HarnessConversationStore) CloseThread(ctx context.Context, threadID harness.ThreadID) error {
	return s.write(ctx, func(mapper HarnessConversationMapper) error { return mapper.CloseThread(ctx, s.key(threadID)) })
}

func (s *HarnessConversationStore) IsFreshContextThread(ctx context.Context, threadID harness.ThreadID) (bool, error) {
	row, err := s.thread(ctx, threadID)
	return row.Fresh, err
}

func (s *HarnessConversationStore) GetThreadParentInfo(ctx context.Context, threadID harness.ThreadID) (*harness.ThreadParentInfo, error) {
	row, err := s.thread(ctx, threadID)
	if err != nil || row.ParentThreadUUID == nil || row.SpawnToolCallID == nil {
		return nil, err
	}
	return &harness.ThreadParentInfo{ThreadID: harness.ThreadID(*row.ParentThreadUUID), ToolCallID: harness.ToolCallID(*row.SpawnToolCallID)}, nil
}

func (s *HarnessConversationStore) GetAncestorChain(ctx context.Context, threadID harness.ThreadID) ([]harness.Ancestor, error) {
	var chain []harness.Ancestor
	for {
		row, err := s.thread(ctx, threadID)
		if err != nil {
			return nil, err
		}
		if row.ParentThreadUUID == nil {
			break
		}
		if row.SpawnMessageSeq == nil {
			return nil, fmt.Errorf("missing spawn order for thread %s", threadID)
		}
		threadID = harness.ThreadID(*row.ParentThreadUUID)
		chain = append(chain, harness.Ancestor{ThreadID: threadID, UpToOrder: *row.SpawnMessageSeq})
	}
	slices.Reverse(chain)
	return chain, nil
}

func (s *HarnessConversationStore) MarkThreadAsCompaction(ctx context.Context, threadID harness.ThreadID) error {
	return s.write(ctx, func(mapper HarnessConversationMapper) error { return mapper.MarkCompaction(ctx, s.key(threadID)) })
}

func (s *HarnessConversationStore) IsCompactionThread(ctx context.Context, threadID harness.ThreadID) (bool, error) {
	row, err := s.thread(ctx, threadID)
	return row.Compaction, err
}

func (s *HarnessConversationStore) GetThreadSpawnOrder(ctx context.Context, threadID harness.ThreadID) (*int64, error) {
	row, err := s.thread(ctx, threadID)
	return row.SpawnMessageSeq, err
}

func (s *HarnessConversationStore) SaveCompactionSummary(ctx context.Context, threadID harness.ThreadID, summary string, upToOrder int64) error {
	if upToOrder < 0 {
		return harness.ErrInvalidHistoryOrder
	}
	return s.write(ctx, func(mapper HarnessConversationMapper) error {
		key := s.key(threadID)
		if _, err := mapper.LockThread(ctx, key); err != nil {
			return mapNoRows(err)
		}
		return mapper.SaveSummary(ctx, key, summary, upToOrder)
	})
}

func (s *HarnessConversationStore) LoadLatestCompactionSummaryUpTo(ctx context.Context, threadID harness.ThreadID, upToOrder *int64) (*harness.CompactionSummary, error) {
	if upToOrder != nil && *upToOrder < 0 {
		return nil, harness.ErrInvalidHistoryOrder
	}
	row, found, err := s.mapper().FindSummary(ctx, s.key(threadID), upToOrder)
	if err != nil || !found {
		return nil, err
	}
	return &harness.CompactionSummary{Summary: row.Summary, UpToOrder: row.UpToSeq}, nil
}

func deleteHarnessConversation(ctx context.Context, executor yourbatis.Executor, session Session) error {
	mapper := NewHarnessConversationMapper(executor)
	scope := harnessConversationScope{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID}
	if err := mapper.DeleteHistory(ctx, scope); err != nil {
		return err
	}
	if err := mapper.DeleteSummaries(ctx, scope); err != nil {
		return err
	}
	if err := NewHarnessStateMapper(executor).DeleteStates(ctx, scope); err != nil {
		return err
	}
	return mapper.DeleteThreads(ctx, scope)
}

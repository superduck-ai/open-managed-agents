package codesessions

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type activationSnapshot struct {
	codeSession db.CodeSession
	events      []db.SessionEvent
}

// ActivateManagedAgentCodeSession 在锁外准备对象，在重新锁定并核对完整历史后发布。
// 准备期间新增的历史会触发重试；发布与 active 切换仍在同一个 Session/Code Session
// 行锁事务中，不允许 realtime cutover 或 termination 插入其中。
func (s *Service) ActivateManagedAgentCodeSession(ctx context.Context, codeSession db.CodeSession) error {
	if s == nil || s.db == nil {
		return db.ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, workerPublicationTimeout)
	defer cancel()
	for {
		err := s.activateSnapshot(ctx, codeSession)
		if !errors.Is(err, errActivationSnapshotChanged) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func loadActivationSnapshot(ctx context.Context, tx db.ManagedAgentActivationTx, codeSession db.CodeSession) (activationSnapshot, error) {
	session, err := tx.LockSessionForEvents(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
	if err != nil {
		return activationSnapshot{}, err
	}
	locked, err := tx.LockInitializingCodeSession(ctx, codeSession.WorkspaceUUID, codeSession.UUID)
	if err != nil {
		return activationSnapshot{}, err
	}
	events, err := tx.ListSessionEventsForActivation(ctx, session)
	return activationSnapshot{codeSession: locked, events: events}, err
}

func (s *Service) activateSnapshot(ctx context.Context, codeSession db.CodeSession) error {
	var snapshot activationSnapshot
	err := s.db.WithManagedAgentActivationTx(ctx, func(tx db.ManagedAgentActivationTx) error {
		var err error
		snapshot, err = loadActivationSnapshot(ctx, tx, codeSession)
		return err
	})
	if err != nil {
		return err
	}
	batch := &inboundPublicationBatch{service: s}
	defer batch.cleanupUnpublished(ctx)
	if err := s.prepareActivation(ctx, snapshot, batch); err != nil {
		return err
	}
	return s.db.WithManagedAgentActivationTx(ctx, func(tx db.ManagedAgentActivationTx) error {
		current, err := loadActivationSnapshot(ctx, tx, codeSession)
		if err != nil {
			return err
		}
		if !snapshot.matches(current) {
			return errActivationSnapshotChanged
		}
		if err := batch.publish(ctx); err != nil {
			return err
		}
		activated, err := tx.ActivateCodeSession(ctx, current.codeSession.UUID, time.Now().UTC())
		if err != nil {
			return err
		}
		if !activated {
			return db.ErrInvalidState
		}
		return nil
	})
}

func (s *Service) prepareActivation(ctx context.Context, snapshot activationSnapshot, batch *inboundPublicationBatch) error {
	configRaw, err := marshalRaw(codeSessionConfig(snapshot.codeSession.Metadata))
	if err != nil {
		return err
	}
	initialize, err := s.prepareInitializeEvent(ctx, snapshot.codeSession, configRaw, snapshot.codeSession.CreatedAt)
	if err != nil {
		return err
	}
	batch.events = append(batch.events, initialize)
	for _, event := range snapshot.events {
		if !maevents.IsPublicWorkerInputEvent(event.EventType) {
			continue
		}
		inbound, err := s.convertSessionEventToInbound(ctx, snapshot.codeSession, event)
		if err != nil {
			return err
		}
		batch.events = append(batch.events, inbound)
	}
	return nil
}

func (s activationSnapshot) matches(other activationSnapshot) bool {
	if !bytes.Equal(s.codeSession.Metadata, other.codeSession.Metadata) || len(s.events) != len(other.events) {
		return false
	}
	for i, event := range s.events {
		// 历史通常只追加；同时比较 payload，避免修订或软删除期间使用过期快照。
		candidate := other.events[i]
		if event.UUID != candidate.UUID || event.EventType != candidate.EventType ||
			!event.ProcessedAt.Equal(candidate.ProcessedAt) || !bytes.Equal(event.Payload, candidate.Payload) {
			return false
		}
	}
	return true
}

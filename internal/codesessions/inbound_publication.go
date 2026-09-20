package codesessions

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const workerPublicationTimeout = time.Minute

// attempted 划分已尝试发布（PubAck 可能丢失）与确定未发布的对象。
// 清理必须在调用方退出行锁事务之后执行；已尝试的对象依赖原定到期任务兜底。
type inboundPublicationBatch struct {
	service   *Service
	events    []preparedInboundEvent
	attempted int
}

func (b *inboundPublicationBatch) publish(ctx context.Context) error {
	for _, event := range b.events {
		b.attempted++
		if err := b.service.publishPreparedInboundEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (b *inboundPublicationBatch) cleanupUnpublished(ctx context.Context) {
	for _, event := range b.events[b.attempted:] {
		b.service.triggerPayloadCleanupNow(ctx, event.cleanupJobID)
	}
}

func (s *Service) publishControlResponse(ctx context.Context, codeSessionID string, payload []byte, source, seed string) error {
	ctx, cancel := context.WithTimeout(ctx, workerPublicationTimeout)
	defer cancel()
	codeSession, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return err
	}
	if !found {
		return db.ErrNotFound
	}
	if codeSession.Status != "active" {
		return db.ErrInvalidState
	}
	prepared, err := s.prepareInboundEvent(ctx, codeSession, payload, source, seed)
	if err != nil {
		return err
	}
	batch := &inboundPublicationBatch{service: s, events: []preparedInboundEvent{prepared}}
	defer batch.cleanupUnpublished(ctx)
	return s.db.WithLockedActiveCodeSession(ctx, codeSessionID, func(db.CodeSession) error {
		return batch.publish(ctx)
	})
}

package codesessions

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry"

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

func (s *Service) publishControlResponse(ctx context.Context, codeSessionID string, workerEpoch int64, payload []byte, source, seed, completedToolID string, wake bool) error {
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
	return s.publishToolHandoff(ctx, codeSession, completedToolID, wake, func(locked db.CodeSession) error {
		if workerEpoch > 0 && locked.CurrentWorkerEpoch != workerEpoch {
			return db.ErrWorkerEpochMismatch
		}
		return batch.publish(ctx)
	})
}

func (s *Service) publishToolHandoff(ctx context.Context, code db.CodeSession, completedToolID string, wake bool, publish func(db.CodeSession) error) error {
	var workerTraceParent *string
	if wake && usesBrainClaimTransport(code) {
		workerTraceParent = new(telemetry.TraceParent(ctx))
	}
	if err := s.db.WithLockedActiveCodeSession(ctx, code.ExternalID, completedToolID, workerTraceParent, publish); err != nil {
		return err
	}
	if workerTraceParent != nil {
		s.notifyBrainWork(ctx, code.ExternalID)
	}
	return nil
}

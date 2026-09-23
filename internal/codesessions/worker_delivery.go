package codesessions

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const workerDeliveryTimeout = 5 * time.Second

type workerDeliveryUpdate struct {
	EventID string
	Status  string
}

type workerDeliveryResult struct {
	Applied int
	Ignored int
}

func (s *Service) applyWorkerDeliveryUpdates(
	ctx context.Context,
	codeSessionID string,
	epoch int64,
	updates []workerDeliveryUpdate,
) (workerDeliveryResult, error) {
	result := workerDeliveryResult{}
	cleanupJobIDs := make([]string, 0)
	// 锁一直持有到 JetStream 确认完成；限定整个批次的等待时间，避免 Redis/NATS
	// 不响应时无限阻塞 register、凭证轮换或 sandbox recovery。
	ackCtx, cancel := context.WithTimeout(ctx, workerDeliveryTimeout)
	err := s.db.WithLockedCodeSessionWorkerEpoch(ackCtx, codeSessionID, epoch, func() (bool, error) {
		var applyErr error
		result, cleanupJobIDs, applyErr = s.applyLockedWorkerDeliveryUpdates(ackCtx, codeSessionID, epoch, updates)
		return result.Applied > 0, applyErr
	})
	cancel()
	// 对象清理会访问 jobs 表，必须在释放 Code Session 事务后执行，避免回调
	// 另取 DB 连接。即使后续 update 失败，已 DoubleAck 的对象仍可以清理。
	for _, cleanupJobID := range cleanupJobIDs {
		s.triggerPayloadCleanupNow(ctx, cleanupJobID)
	}
	return result, err
}

func (s *Service) applyLockedWorkerDeliveryUpdates(
	ctx context.Context,
	codeSessionID string,
	epoch int64,
	updates []workerDeliveryUpdate,
) (workerDeliveryResult, []string, error) {
	result := workerDeliveryResult{}
	cleanupJobIDs := make([]string, 0)
	// 整批映射先一次取回，避免在行锁内逐 event 串行访问 ACK store。
	eventIDs := make([]string, len(updates))
	for index, update := range updates {
		eventIDs[index] = update.EventID
	}
	references, err := s.workerEventAcks.GetMany(ctx, codeSessionID, epoch, eventIDs)
	if err != nil {
		return result, cleanupJobIDs, err
	}
	for _, update := range updates {
		reference, found := references[update.EventID]
		if !found {
			result.Ignored++
			continue
		}
		switch update.Status {
		case "received", "processing":
			if err := s.workerEvents.InProgress(ctx, reference.AckSubject); err != nil {
				return result, cleanupJobIDs, workerEventUnavailable(err)
			}
			if err := s.workerEventAcks.Refresh(ctx, codeSessionID, epoch, update.EventID); err != nil {
				return result, cleanupJobIDs, err
			}
		case "processed":
			if err := s.workerEvents.DoubleAck(ctx, reference.AckSubject); err != nil {
				return result, cleanupJobIDs, workerEventUnavailable(err)
			}
			if err := s.workerEventAcks.Delete(ctx, codeSessionID, epoch, update.EventID); err != nil {
				s.logger.WarnContext(ctx, "delete processed worker event ACK", "code_session_id", codeSessionID, "event_id", update.EventID, "error", err)
			}
			if reference.CleanupJobID != "" {
				cleanupJobIDs = append(cleanupJobIDs, reference.CleanupJobID)
			}
		default:
			return result, cleanupJobIDs, db.ErrInvalidState
		}
		result.Applied++
	}
	return result, cleanupJobIDs, nil
}

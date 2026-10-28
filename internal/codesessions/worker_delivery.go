package codesessions

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

const workerDeliveryTimeout = 5 * time.Second

type workerDeliveryUpdate struct {
	EventID string
	Status  string
}

type workerDeliveryResult struct {
	Applied int
	Ignored int
	settled []settledWorkerDelivery
}

type settledWorkerDelivery struct {
	update    workerDeliveryUpdate
	outcome   string
	reference workerevents.AckRef
}

func (s *Service) applyWorkerDeliveryUpdates(
	ctx context.Context,
	codeSessionID string,
	epoch int64,
	updates []workerDeliveryUpdate,
) (workerDeliveryResult, error) {
	ackCtx, cancel := context.WithTimeout(ctx, workerDeliveryTimeout)
	defer cancel()
	result := workerDeliveryResult{}
	var references map[string]workerevents.AckRef
	var cleanupJobIDs []string
	needsProcessing := false
	apply := func() (bool, error) {
		var err error
		result, cleanupJobIDs, err = s.applyLockedWorkerDeliveryUpdates(ackCtx, codeSessionID, epoch, updates, references)
		return result.Applied > 0, err
	}
	err := s.db.WithLockedCodeSessionWorkerEpoch(ackCtx, codeSessionID, epoch, func() (bool, error) {
		eventIDs := make([]string, len(updates))
		for i, update := range updates {
			eventIDs[i] = update.EventID
		}
		var err error
		references, err = s.workerEventAcks.GetMany(ackCtx, codeSessionID, epoch, eventIDs)
		if err != nil {
			return false, err
		}
		for _, update := range updates {
			if references[update.EventID].PublicEventID != "" && (update.Status == "processing" || update.Status == "processed") {
				needsProcessing = true
			}
		}
		if needsProcessing {
			return false, nil
		}
		return apply()
	})
	if err == nil && needsProcessing {
		err = s.publishProcessingInputs(ackCtx, codeSessionID, epoch, updates, references)
		if err == nil {
			err = s.db.WithLockedCodeSessionWorkerEpoch(ackCtx, codeSessionID, epoch, apply)
		}
	}
	if err != nil {
		recordFailedWorkerDeliveryUpdates(ctx, updates, err)
	} else {
		observeSettledWorkerDeliveries(ctx, codeSessionID, result.settled)
	}
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
	references map[string]workerevents.AckRef,
) (workerDeliveryResult, []string, error) {
	result := workerDeliveryResult{}
	cleanupJobIDs := make([]string, 0)
	for _, update := range updates {
		reference, found := references[update.EventID]
		if !found {
			result.settled = append(result.settled, settledWorkerDelivery{update: update, outcome: "ignored"})
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
		result.settled = append(result.settled, settledWorkerDelivery{update: update, outcome: "applied", reference: reference})
		result.Applied++
	}
	return result, cleanupJobIDs, nil
}

func observeSettledWorkerDeliveries(ctx context.Context, codeSessionID string, settled []settledWorkerDelivery) {
	for _, delivery := range settled {
		if delivery.outcome == "applied" && delivery.update.Status == "processed" {
			observeWorkerEventAck(ctx, codeSessionID, delivery.update.EventID, delivery.reference)
		}
		recordWorkerEventUpdate(ctx, delivery.update.Status, delivery.outcome)
	}
}

func recordFailedWorkerDeliveryUpdates(ctx context.Context, updates []workerDeliveryUpdate, err error) {
	outcome := telemetry.Outcome(err)
	if ctx.Err() != nil {
		outcome = telemetry.OutcomeCanceled
	}
	for _, update := range updates {
		recordWorkerEventUpdate(ctx, update.Status, outcome)
	}
}

func (s *Service) publishProcessingInputs(ctx context.Context, codeSessionID string, epoch int64, updates []workerDeliveryUpdate, references map[string]workerevents.AckRef) error {
	var inputIDs []string
	for _, update := range updates {
		if update.Status != "processing" && update.Status != "processed" {
			continue
		}
		if id := references[update.EventID].PublicEventID; id != "" {
			inputIDs = append(inputIDs, id)
		}
	}
	if len(inputIDs) == 0 || s.sink == nil {
		return nil
	}
	record, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return err
	}
	if !found {
		return db.ErrNotFound
	}
	return s.sink.ProcessCodeSessionInputs(ctx, record, epoch, inputIDs)
}

package codesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

const (
	workerEventExpiryInterval = time.Minute
	workerEventExpiryBatch    = 512
)

// WorkerEventExpiryWorker enforces the logical retention policy against
// unprocessed events retained by JetStream.
type WorkerEventExpiryWorker struct {
	service *Service
	logger  *slog.Logger
	cursor  uint64
}

func NewWorkerEventExpiryWorker(service *Service, logger *slog.Logger) *WorkerEventExpiryWorker {
	return &WorkerEventExpiryWorker{service: service, logger: logging.LoggerOrDefault(logger)}
}

func (w *WorkerEventExpiryWorker) Start(ctx context.Context) {
	if w == nil || w.service == nil || w.service.db == nil || w.service.workerEvents == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(workerEventExpiryInterval)
		defer ticker.Stop()
		for {
			if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.ErrorContext(ctx, "expire code session worker events", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *WorkerEventExpiryWorker) RunOnce(ctx context.Context) error {
	now := time.Now().UTC()
	events, nextCursor, err := w.service.workerEvents.ScanExpired(ctx, w.cursor, workerEventExpiryBatch, now)
	if err != nil {
		return fmt.Errorf("scan expired JetStream events: %w", err)
	}
	w.cursor = nextCursor
	seen := make(map[string]struct{}, len(events))
	var errs []error
	for _, event := range events {
		if _, found := seen[event.Envelope.CodeSessionID]; found {
			continue
		}
		seen[event.Envelope.CodeSessionID] = struct{}{}
		if err := w.expireSession(ctx, event.Envelope); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (w *WorkerEventExpiryWorker) expireSession(ctx context.Context, envelope workerevents.EnvelopeV1) error {
	session, found, err := w.service.db.GetCodeSession(ctx, envelope.CodeSessionID)
	if err != nil {
		return err
	}
	if !found {
		// Session 记录已删除时仍需清理 subject 与外置对象；terminate 对空租户
		// 范围返回 ErrNotFound，由统一处置策略容忍。
		session = db.CodeSession{ExternalID: envelope.CodeSessionID}
	}
	w.service.expireWorkerEvent(ctx, session, envelope, "")
	return nil
}

// expireWorkerEvent 执行一条过期事件的统一处置：终止本次投递（若有）、加速外置
// payload 的对象清理、终止对应 Code Session 并清空其 worker-event subject。
// SSE handler 与过期 worker 共用该策略，两处行为不会漂移。
func (s *Service) expireWorkerEvent(ctx context.Context, codeSession db.CodeSession, envelope workerevents.EnvelopeV1, ackSubject string) {
	s.logger.ErrorContext(ctx, "code session worker event expired", "code_session_id", codeSession.ExternalID, "event_id", envelope.EventID, "expires_at", envelope.ExpiresAt)
	if ackSubject != "" {
		_ = s.workerEvents.Term(ctx, ackSubject)
	}
	if envelope.PayloadRef != nil {
		s.triggerPayloadCleanupNow(ctx, envelope.PayloadRef.CleanupJobID)
	}
	if err := s.db.TerminateManagedAgentCodeSession(ctx, codeSession.OrganizationUUID, codeSession.WorkspaceUUID, codeSession.ExternalID); err != nil && !errors.Is(err, db.ErrNotFound) {
		s.logger.ErrorContext(ctx, "terminate code session after worker event expiry", "code_session_id", codeSession.ExternalID, "error", err)
	}
	if err := s.workerEvents.PurgeSession(ctx, codeSession.ExternalID); err != nil {
		s.logger.ErrorContext(ctx, "purge expired code session worker events", "code_session_id", codeSession.ExternalID, "error", err)
	}
}

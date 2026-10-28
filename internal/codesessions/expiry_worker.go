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
	// 即使后续扫描失败，也要记录本批已删除的非法消息。不要记录原始 subject 或正文。
	for _, event := range events {
		if event.InvalidSubject {
			w.logger.ErrorContext(ctx, "removed worker event with invalid subject", "stream_sequence", event.StreamSequence)
		}
	}
	if err != nil {
		return fmt.Errorf("scan expired JetStream events: %w", err)
	}
	seen := make(map[string]struct{}, len(events))
	var errs []error
	for _, event := range events {
		if event.InvalidSubject {
			continue
		}
		if event.DecodeError != nil {
			w.logger.ErrorContext(ctx, "invalid stored worker event", "stream_sequence", event.StreamSequence, "code_session_id", event.Envelope.CodeSessionID, "error", event.DecodeError)
			// 无法信任坏 envelope 的 expires_at，以 JetStream 存储时间 + 30 天兜底。
			// 在期限前只告警，不 ACK、不跳过该 Session，但不阻塞其他 Session 扫描。
			if !event.Envelope.IsExpired(now) {
				continue
			}
		}
		if _, found := seen[event.Envelope.CodeSessionID]; found {
			continue
		}
		seen[event.Envelope.CodeSessionID] = struct{}{}
		if err := w.expireSession(ctx, event.Envelope); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	w.cursor = nextCursor
	return nil
}

func (w *WorkerEventExpiryWorker) expireSession(ctx context.Context, envelope workerevents.EnvelopeV1) error {
	session, found, err := w.service.db.GetCodeSession(ctx, envelope.CodeSessionID)
	if err != nil {
		return err
	}
	if !found {
		// 没有记录便不执行带空租户 UUID 的 SQL；对象仍有上传前创建的兜底任务。
		return w.service.workerEvents.PurgeSession(ctx, envelope.CodeSessionID)
	}
	return w.service.expireWorkerEvent(ctx, session, envelope)
}

// expireWorkerEvent 与普通 termination 共用先终止 PG、再清理队列的顺序。
// PG 失败时不能 TERM 或 purge，否则重试扫描会失去尚未终止 Session 的依据。
func (s *Service) expireWorkerEvent(ctx context.Context, codeSession db.CodeSession, envelope workerevents.EnvelopeV1) error {
	s.logger.ErrorContext(ctx, "code session worker event expired", "code_session_id", codeSession.ExternalID, "event_id", envelope.EventID, "expires_at", envelope.ExpiresAt)
	if err := s.TerminateManagedAgentCodeSession(ctx, db.Session{
		OrganizationUUID: codeSession.OrganizationUUID, WorkspaceUUID: codeSession.WorkspaceUUID,
	}, codeSession.ExternalID); err != nil {
		return err
	}
	if envelope.PayloadRef != nil {
		s.triggerPayloadCleanupNow(ctx, envelope.PayloadRef.CleanupJobID)
	}
	return nil
}

package codesessions

import (
	"context"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
	"io"
	"net/http"
)

func (s *Service) notifyDSHTask(ctx context.Context, codeSessionID string) {
	if signal, ok := s.workerEvents.(workerevents.TaskWorkSignal); ok {
		if err := signal.NotifyTaskWork(ctx, codeSessionID); err != nil {
			s.logger.WarnContext(ctx, "notify DSR task availability", "code_session_id", codeSessionID, "error", err)
		}
	}
}

func (s *Service) subscribeDSHTask(ctx context.Context, codeSessionID string) <-chan struct{} {
	if signal, ok := s.workerEvents.(workerevents.TaskWorkSignal); ok {
		notifications, err := signal.SubscribeTaskWork(ctx, codeSessionID)
		if err == nil {
			return notifications
		}
		s.logger.WarnContext(ctx, "subscribe DSR task availability", "code_session_id", codeSessionID, "error", err)
	}
	return nil
}

func writeDSHTaskAvailable(w io.Writer, flusher http.Flusher) error {
	if _, err := io.WriteString(w, "event: task.available\ndata: {}\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

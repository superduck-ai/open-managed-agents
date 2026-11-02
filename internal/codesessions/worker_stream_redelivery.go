package codesessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type workerStreamDeliveries struct {
	ackSubjects map[string]struct{}
}

func (d *workerStreamDeliveries) track(ackSubject string) {
	if ackSubject == "" {
		return
	}
	if d.ackSubjects == nil {
		d.ackSubjects = make(map[string]struct{})
	}
	d.ackSubjects[ackSubject] = struct{}{}
}

func (s *Service) redeliverAbandonedWorkerEvents(codeSession db.CodeSession, epoch int64, deliveries *workerStreamDeliveries) {
	ctx, cancel := context.WithTimeout(context.Background(), workerDeliveryTimeout)
	defer cancel()
	if err := nakAbandonedWorkerEvents(ctx, s.workerEvents, s.db.ValidateCodeSessionWorkerEpoch, codeSession.ExternalID, epoch, deliveries); err != nil {
		s.logger.WarnContext(ctx, "redeliver abandoned worker events", "code_session_id", codeSession.ExternalID, "worker_epoch", epoch, "error", err)
	}
}

func nakAbandonedWorkerEvents(ctx context.Context, broker workerevents.Broker, validate func(context.Context, string, int64) error,
	codeSessionID string, epoch int64, deliveries *workerStreamDeliveries) error {

	if len(deliveries.ackSubjects) == 0 {
		return nil
	}
	err := validate(ctx, codeSessionID, epoch)
	switch {
	case err == nil, errors.Is(err, db.ErrNotFound):
		return nil
	case !errors.Is(err, db.ErrWorkerEpochMismatch):
		return fmt.Errorf("validate worker epoch: %w", err)
	}
	var failures []error
	for ackSubject := range deliveries.ackSubjects {
		if err := broker.Nak(ctx, ackSubject); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

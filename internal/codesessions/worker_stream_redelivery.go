package codesessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

const workerStreamDrainTimeout = time.Second

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

func (d *workerStreamDeliveries) accept(ackSubject string, validate func() error) error {
	d.track(ackSubject)
	return validate()
}

func (d *workerStreamDeliveries) drain(ctx context.Context, subscription workerevents.Subscription) {
	for {
		select {
		case delivery, open := <-subscription.Messages():
			if !open {
				return
			}
			d.track(delivery.AckSubject)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) redeliverAbandonedWorkerEvents(codeSession db.CodeSession, epoch int64, subscription workerevents.Subscription, deliveries *workerStreamDeliveries) {
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), workerStreamDrainTimeout)
	deliveries.drain(drainCtx, subscription)
	cancelDrain()
	ctx, cancel := context.WithTimeout(context.Background(), workerDeliveryTimeout)
	defer cancel()
	validate := func(ctx context.Context, codeSessionID string, epoch int64) error {
		return s.db.ValidateCodeSessionWorkerClaim(ctx, codeSession.OrganizationUUID, codeSession.WorkspaceUUID, codeSessionID, epoch)
	}
	if err := nakAbandonedWorkerEvents(ctx, s.workerEvents, validate, codeSession.ExternalID, epoch, deliveries); err != nil {
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
	case !errors.Is(err, db.ErrWorkerEpochMismatch) && !errors.Is(err, db.ErrWorkerClaimReleased):
		return fmt.Errorf("validate worker claim: %w", err)
	}
	var failures []error
	for ackSubject := range deliveries.ackSubjects {
		if err := broker.Nak(ctx, ackSubject); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

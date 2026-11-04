package codesessions

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type dshDeliveryStore interface {
	LeaseDSHDeliveries(context.Context) ([]db.DSHDeliveryLease, error)
	CompleteDSHDelivery(context.Context, db.DSHDeliveryLease) error
	RetryDSHDelivery(context.Context, db.DSHDeliveryLease) error
}

type dshAcknowledger interface {
	DoubleAck(context.Context, string) error
}

type DSHDeliveryWorker struct {
	store  dshDeliveryStore
	broker dshAcknowledger
	logger *slog.Logger
}

func NewDSHDeliveryWorker(store dshDeliveryStore, broker dshAcknowledger, logger *slog.Logger) *DSHDeliveryWorker {
	return &DSHDeliveryWorker{store: store, broker: broker, logger: logging.LoggerOrDefault(logger)}
}

func (w *DSHDeliveryWorker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.ErrorContext(ctx, "settle DSR input deliveries", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *DSHDeliveryWorker) RunOnce(ctx context.Context) error {
	items, err := w.store.LeaseDSHDeliveries(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		attemptCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := w.broker.DoubleAck(attemptCtx, item.AckSubject)
		cancel()
		if err != nil {
			w.logger.WarnContext(ctx, "retry DSR input acknowledgement", "delivery_uuid", item.UUID, "input_id", item.InputID)
			if retryErr := w.store.RetryDSHDelivery(ctx, item); retryErr != nil {
				failures = append(failures, retryErr)
			}
			continue
		}
		if err := w.store.CompleteDSHDelivery(ctx, item); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

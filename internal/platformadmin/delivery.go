package platformadmin

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type deliveryStore interface {
	LeasePlatformAdminDelivery(context.Context) (db.PlatformAdminDelivery, bool, error)
	PlatformAdminDeliveryReady(context.Context, string, int) (bool, error)
	FinishPlatformAdminDelivery(context.Context, string, int, string) error
}

type deliveryMailer interface {
	NotifyPlatform(context.Context, string, string, time.Time) string
}
type DeliveryWorker struct {
	db     deliveryStore
	mailer deliveryMailer
	logger *slog.Logger
}

func NewDeliveryWorker(database deliveryStore, mailer deliveryMailer, logger *slog.Logger) *DeliveryWorker {
	return &DeliveryWorker{database, mailer, logging.LoggerOrDefault(logger)}
}
func (w *DeliveryWorker) Start(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	var workers sync.WaitGroup
	workers.Go(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			for range 20 {
				processed, err := w.RunOnce(ctx)
				if err != nil && ctx.Err() == nil {
					w.logger.ErrorContext(ctx, "platform invitation delivery failed", "error", err)
				}
				if !processed || err != nil || ctx.Err() != nil {
					break
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	return func() { cancel(); workers.Wait() }
}

func (w *DeliveryWorker) RunOnce(ctx context.Context) (bool, error) {
	delivery, found, err := w.db.LeasePlatformAdminDelivery(ctx)
	if err != nil || !found {
		return false, err
	}
	ready, err := w.db.PlatformAdminDeliveryReady(ctx, delivery.UUID, delivery.Attempts)
	if err != nil {
		return true, err
	}
	status := "cancelled"
	if ready {
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		status = w.mailer.NotifyPlatform(sendCtx, delivery.OrganizationUUID, delivery.Recipient, delivery.ExpiresAt)
		cancel()
		if status != "sent" {
			status = "failed"
		}
	}
	return true, w.db.FinishPlatformAdminDelivery(ctx, delivery.UUID, delivery.Attempts, status)
}

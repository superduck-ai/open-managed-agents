package platformadmin

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const DeliveryQueue = "platform_invitation_delivery"

type deliveryArgs struct {
	DeliveryUUID string `json:"delivery_uuid"`
}

func (deliveryArgs) Kind() string { return DeliveryQueue }
func (deliveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: DeliveryQueue, MaxAttempts: 5}
}

type deliveryStore interface {
	StartPlatformAdminDelivery(context.Context, string) (db.PlatformAdminDelivery, bool, error)
	FinishPlatformAdminDelivery(context.Context, string, string) error
}
type deliveryMailer interface {
	NotifyPlatform(context.Context, string, string, time.Time) string
}

type DeliveryWorker struct {
	river.WorkerDefaults[deliveryArgs]
	db     deliveryStore
	mailer deliveryMailer
}

func RegisterDeliveryWorker(workers *river.Workers, database *db.DB, mailer deliveryMailer) {
	river.AddWorker(workers, &DeliveryWorker{db: database, mailer: mailer})
}
func (w *DeliveryWorker) Timeout(*river.Job[deliveryArgs]) time.Duration { return 30 * time.Second }
func (w *DeliveryWorker) Work(ctx context.Context, job *river.Job[deliveryArgs]) error {
	delivery, found, err := w.db.StartPlatformAdminDelivery(ctx, job.Args.DeliveryUUID)
	if err != nil || !found {
		return err
	}
	status := w.mailer.NotifyPlatform(ctx, delivery.OrganizationUUID, delivery.Recipient, delivery.ExpiresAt)
	if status != "sent" {
		status = "failed"
	}
	if err := w.db.FinishPlatformAdminDelivery(ctx, delivery.UUID, status); err != nil {
		return err
	}
	if status == "failed" {
		return errors.New("邀请邮件投递失败")
	}
	return nil
}
func (s *Service) ConfigureDelivery(client *river.Client[*sql.Tx]) { s.delivery = client }
func (s *Service) enqueueInvite(ctx context.Context, tx *db.PlatformAdminTx, org, invitation string) error {
	if s.delivery == nil {
		return ErrUnavailable
	}
	delivery, err := tx.EnqueueInviteDelivery(ctx, org, invitation)
	if err != nil {
		return err
	}
	_, err = s.delivery.InsertTx(ctx, tx.SQLTx(), deliveryArgs{DeliveryUUID: delivery}, nil)
	return err
}

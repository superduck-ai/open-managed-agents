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
	InvitationUUID   string    `json:"invitation_uuid"`
	OrganizationUUID string    `json:"organization_uuid"`
	Recipient        string    `json:"recipient"`
	InvitedAt        time.Time `json:"invited_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

func (deliveryArgs) Kind() string { return DeliveryQueue }
func (deliveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: DeliveryQueue, MaxAttempts: 5}
}

type deliveryStore interface {
	StartInvitationDelivery(context.Context, db.InvitationDelivery) (bool, error)
	FinishInvitationDelivery(context.Context, db.InvitationDelivery, string) error
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
	snapshot := db.InvitationDelivery{InvitationUUID: job.Args.InvitationUUID, OrganizationUUID: job.Args.OrganizationUUID, Recipient: job.Args.Recipient, InvitedAt: job.Args.InvitedAt, ExpiresAt: job.Args.ExpiresAt}
	found, err := w.db.StartInvitationDelivery(ctx, snapshot)
	if err != nil || !found {
		return err
	}
	status := w.mailer.NotifyPlatform(ctx, snapshot.OrganizationUUID, snapshot.Recipient, snapshot.ExpiresAt)
	if status != "sent" {
		status = "failed"
	}
	if err := w.db.FinishInvitationDelivery(ctx, snapshot, status); err != nil {
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
	_, err = s.delivery.InsertTx(ctx, tx.SQLTx(), deliveryArgs{InvitationUUID: delivery.UUID, OrganizationUUID: org, Recipient: delivery.Email, InvitedAt: delivery.InvitedAt, ExpiresAt: delivery.ExpiresAt}, nil)
	return err
}

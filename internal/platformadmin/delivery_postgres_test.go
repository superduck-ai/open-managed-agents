//go:build integration

package platformadmin

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type retryMailer struct{ sends atomic.Int32 }

func (m *retryMailer) NotifyPlatform(context.Context, string, string, time.Time) string {
	if m.sends.Add(1) == 1 {
		return "failed"
	}
	return "sent"
}

func TestInvitationRiverPostgres(t *testing.T) {
	database := administrationDatabase(t)
	service := administrationService(t, database)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	account := uuid.NewV4().String()
	var org string
	if err := database.WithPlatformAuthTx(ctx, func(tx db.PlatformAuthTxStore) error {
		row, err := tx.InsertOrganization(ctx, db.PlatformAuthOrganizationInput{Name: "投递验收"})
		if err != nil {
			return err
		}
		org = row.UUID
		_, err = tx.InsertUser(ctx, db.PlatformAuthUserInput{UUID: account, ExternalID: "user_" + account, OrganizationUUID: org, Email: "operator@example.com", Role: "admin"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAdministrator(ctx, account, "验收", "", true); err != nil {
		t.Fatal(err)
	}
	before, err := database.GetPlatformAdminOrganization(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := database.SQLDB().QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	broken, err := river.NewClient[*sql.Tx](riverdatabasesql.New(database.SQLDB()), &river.Config{Schema: "missing_invitation_queue"})
	if err != nil {
		t.Fatal(err)
	}
	original := service.delivery
	service.ConfigureDelivery(broken)
	input := InviteChange{Change: Change{ExpectedVersion: before.Version}, Email: "rollback@example.com", Role: "user"}
	if _, err := service.CreateInvite(ctx, Actor{AccountUUID: account}, org, input); err == nil {
		t.Fatal("队列写入失败未回滚")
	}
	service.ConfigureDelivery(original)
	rows, err := database.ListPlatformAdminInvites(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org, Limit: 100})
	if err != nil || len(rows) != 0 {
		t.Fatalf("邀请未回滚：%+v %v", rows, err)
	}
	after, err := database.GetPlatformAdminOrganization(ctx, org)
	if err != nil || after.Version != before.Version {
		t.Fatalf("版本未回滚：%+v %v", after, err)
	}
	input.Email = "retry@example.com"
	if _, err := service.CreateInvite(ctx, Actor{AccountUUID: account}, org, input); err != nil {
		t.Fatal(err)
	}
	jobs, err := original.JobList(ctx, river.NewJobListParams().Kinds(DeliveryQueue))
	if err != nil || len(jobs.Jobs) != 1 || jobs.Jobs[0].MaxAttempts != 5 {
		t.Fatalf("事务未入队：%+v %v", jobs, err)
	}
	workers := river.NewWorkers()
	mailer := &retryMailer{}
	river.AddWorker(workers, &DeliveryWorker{db: database, mailer: mailer})
	client, err := river.NewClient[*sql.Tx](riverdatabasesql.New(database.SQLDB()), &river.Config{Schema: schema, Workers: workers, Queues: map[string]river.QueueConfig{DeliveryQueue: {MaxWorkers: 1}}, FetchPollInterval: 100 * time.Millisecond, FetchCooldown: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := client.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed)
	defer unsubscribe()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := client.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	}()
	retried := false
	for {
		select {
		case event := <-events:
			if event.Job.ID != jobs.Jobs[0].ID {
				continue
			}
			if event.Kind == river.EventKindJobFailed {
				if retried || (event.Job.State != rivertype.JobStateRetryable && event.Job.State != rivertype.JobStateAvailable) {
					t.Fatalf("重试状态错误：%+v", event.Job)
				}
				rows, err := database.ListPlatformAdminInvites(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org, Limit: 10})
				if err != nil || len(rows) != 1 || rows[0].DeliveryStatus != "failed" {
					t.Fatalf("失败未回读：%+v %v", rows, err)
				}
				retried = true
				if !event.Job.ScheduledAt.After(*event.Job.AttemptedAt) {
					t.Fatal("失败任务未安排自动重试")
				}
				continue
			}
			rows, err := database.ListPlatformAdminInvites(ctx, db.PlatformAdminPageQuery{OrganizationUUID: org, Limit: 10})
			if err != nil || len(rows) != 1 || rows[0].DeliveryStatus != "sent" || !retried || mailer.sends.Load() != 2 || event.Job.Attempt != 2 {
				t.Fatalf("重试完成结果：%+v %v attempts=%d sends=%d", rows, err, event.Job.Attempt, mailer.sends.Load())
			}
			return
		case <-ctx.Done():
			t.Fatal("等待 River 投递结果超时")
		}
	}
}

package platformadmin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type deliveryStub struct {
	ready    bool
	found    bool
	err      error
	status   string
	finished string
	sends    int
}

func (s *deliveryStub) LeasePlatformAdminDelivery(context.Context) (db.PlatformAdminDelivery, bool, error) {
	return db.PlatformAdminDelivery{UUID: "delivery", OrganizationUUID: "organization", Attempts: 2}, s.found, s.err
}

func (s *deliveryStub) PlatformAdminDeliveryReady(context.Context, string, int) (bool, error) {
	return s.ready, nil
}

func (s *deliveryStub) FinishPlatformAdminDelivery(_ context.Context, id string, attempts int, status string) error {
	if id != "delivery" || attempts != 2 {
		return errors.New("投递租约身份错误")
	}
	s.finished = status
	return nil
}

func (s *deliveryStub) NotifyPlatform(ctx context.Context, _, _ string, _ time.Time) string {
	s.sends++
	if _, ok := ctx.Deadline(); !ok {
		return "missing_deadline"
	}
	return s.status
}

func TestInvitationDeliveryWorker(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stub       deliveryStub
		wantStatus string
		wantSends  int
	}{
		{"领取失败不发送", deliveryStub{err: errors.New("database unavailable")}, "", 0},
		{"撤销后不发送", deliveryStub{found: true}, "cancelled", 0},
		{"发送失败可重试", deliveryStub{found: true, ready: true, status: "failed"}, "failed", 1},
		{"未配置邮件不记成功", deliveryStub{found: true, ready: true, status: "not_configured"}, "failed", 1},
		{"空队列", deliveryStub{}, "", 0},
		{"成功发送", deliveryStub{found: true, ready: true, status: "sent"}, "sent", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := NewDeliveryWorker(&tc.stub, &tc.stub, nil)
			processed, err := worker.RunOnce(t.Context())
			if !errors.Is(err, tc.stub.err) || processed != tc.stub.found || tc.stub.finished != tc.wantStatus || tc.stub.sends != tc.wantSends {
				t.Fatalf("消费结果：processed=%v err=%v status=%s sends=%d", processed, err, tc.stub.finished, tc.stub.sends)
			}
		})
	}
}

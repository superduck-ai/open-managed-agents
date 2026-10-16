package platformadmin

import (
	"context"
	"errors"
	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
	"time"
)

type deliveryStub struct {
	found    bool
	err      error
	status   string
	finished string
	sends    int
}

func (s *deliveryStub) StartPlatformAdminDelivery(context.Context, string) (db.PlatformAdminDelivery, bool, error) {
	return db.PlatformAdminDelivery{UUID: "delivery", OrganizationUUID: "organization"}, s.found, s.err
}
func (s *deliveryStub) FinishPlatformAdminDelivery(_ context.Context, _, status string) error {
	s.finished = status
	return nil
}
func (s *deliveryStub) NotifyPlatform(context.Context, string, string, time.Time) string {
	s.sends++
	return s.status
}
func TestInvitationDeliveryWorker(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stub       deliveryStub
		wantStatus string
		wantSends  int
		wantErr    bool
	}{
		{"查询失败返回 River 重试", deliveryStub{err: errors.New("数据库故障")}, "", 0, true},
		{"撤销或过期不发送", deliveryStub{}, "", 0, false},
		{"发送失败返回 River 重试", deliveryStub{found: true, status: "failed"}, "failed", 1, true},
		{"邮件未配置不记成功", deliveryStub{found: true, status: "not_configured"}, "failed", 1, true},
		{"成功发送", deliveryStub{found: true, status: "sent"}, "sent", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := &DeliveryWorker{db: &tc.stub, mailer: &tc.stub}
			err := worker.Work(t.Context(), &river.Job[deliveryArgs]{Args: deliveryArgs{DeliveryUUID: "delivery"}})
			if (err != nil) != tc.wantErr || tc.stub.finished != tc.wantStatus || tc.stub.sends != tc.wantSends {
				t.Fatalf("投递结果：%v %+v", err, tc.stub)
			}
			if worker.Timeout(nil) != 30*time.Second || (deliveryArgs{}).InsertOpts().MaxAttempts != 5 {
				t.Fatal("投递超时或重试配置不正确")
			}
		})
	}
}

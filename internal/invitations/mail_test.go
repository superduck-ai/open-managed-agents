package invitations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

type mailFixture struct {
	err       error
	calls     int
	recipient string
	message   string
}

func (f *mailFixture) GetPlatformOrganization(context.Context, string) (*platform.OrganizationRecord, error) {
	return &platform.OrganizationRecord{Name: "研发 <script>alert(1)</script>"}, nil
}
func (f *mailFixture) GetAdminUser(context.Context, string, string) (db.AdminUser, error) {
	return db.AdminUser{Email: "admin@example.com"}, nil
}
func (f *mailFixture) SendMessage(_ context.Context, recipient string, message []byte) error {
	f.calls++
	f.recipient, f.message = recipient, string(message)
	return f.err
}

func TestInvitationMailDelivery(t *testing.T) {
	principal := auth.Principal{OrganizationUUID: "org", UserExternalID: "user"}
	expiry := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	t.Run("未配置不发送也不声称成功", func(t *testing.T) {
		mailer := NewMailer(config.AuthConfig{}, nil, nil)
		if got := mailer.Notify(t.Context(), principal, "a@example.com", expiry); got != "not_configured" {
			t.Fatal(got)
		}
	})
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "SMTP失败允许重发", false: "SMTP接受且模板转义"}[fail], func(t *testing.T) {
			fixture := &mailFixture{}
			if fail {
				fixture.err = errors.New("SMTP failure")
			}
			mailer := NewMailer(config.AuthConfig{ConsoleURL: "https://oma.example.com", SMTP: config.EmailSMTPConfig{Username: "sender@example.com"}}, fixture, nil)
			mailer.sender = fixture
			want := "sent"
			if fail {
				want = "failed"
			}
			if got := mailer.Notify(t.Context(), principal, "recipient@example.com", expiry); got != want {
				t.Fatal(got)
			}
			if fixture.calls != 1 || fixture.recipient != "recipient@example.com" {
				t.Fatal("未向邀请接收人发送")
			}
			for _, part := range []string{"admin@example.com", "2026-10-01 08:00 UTC", "https://oma.example.com/invites", "Accept invitation", `lang="en"`, "Opening this link does not automatically accept the invitation.", "&lt;script&gt;"} {
				if !strings.Contains(fixture.message, part) {
					t.Fatalf("邮件缺少 %s", part)
				}
			}
			if strings.Contains(fixture.message, "请使用") {
				t.Fatal("email retained untranslated instructions")
			}
			if strings.Contains(fixture.message, "<script>") {
				t.Fatal("组织名未转义")
			}
			fixture.err = nil
			if got := mailer.Notify(t.Context(), principal, "recipient@example.com", expiry); got != "sent" {
				t.Fatal(got)
			}
		})
	}
}

func TestInvitationMailAllowedEmailDomains(t *testing.T) {
	for _, test := range []struct {
		name      string
		domains   []string
		recipient string
		want      string
	}{
		{"非允许域名", []string{"cvte.com", "gmail.com"}, "user@example.com", "failed"},
		{"子域名", []string{"cvte.com"}, "user@sub.cvte.com", "failed"},
		{"伪装后缀", []string{"cvte.com"}, "user@cvte.com.evil.com", "failed"},
		{"相似域名", []string{"cvte.com"}, "user@evilcvte.com", "failed"},
		{"公司邮箱", []string{"cvte.com", "gmail.com"}, "user@cvte.com", "sent"},
		{"个人邮箱", []string{"cvte.com", "gmail.com"}, "user@gmail.com", "sent"},
		{"域名大小写", []string{"cvte.com"}, "user@CVTE.COM", "sent"},
		{"显式子域名", []string{"sub.cvte.com"}, "user@sub.cvte.com", "sent"},
		{"未配置白名单", nil, "user@example.com", "sent"},
		{"空白名单", []string{}, "user@example.com", "sent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &mailFixture{}
			mailer := NewMailer(config.AuthConfig{AllowedEmailDomains: test.domains}, fixture, nil)
			mailer.sender = fixture
			if test.want == "failed" {
				mailer.db = nil
			}
			for range 2 {
				if got := mailer.Notify(t.Context(), auth.Principal{OrganizationUUID: "org"}, test.recipient, time.Now()); got != test.want {
					t.Fatalf("投递状态 = %q，期望 %q", got, test.want)
				}
			}
			wantCalls := 0
			if test.want == "sent" {
				wantCalls = 2
			}
			if fixture.calls != wantCalls {
				t.Fatalf("发送次数 = %d，期望 %d", fixture.calls, wantCalls)
			}
		})
	}
}

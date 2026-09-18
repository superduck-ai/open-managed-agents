package invitations

import (
	"bytes"
	"context"
	"html/template"
	"log/slog"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
)

type mailStore interface {
	GetPlatformOrganization(context.Context, string) (*platform.OrganizationRecord, error)
	GetAdminUser(context.Context, string, string) (db.AdminUser, error)
}

// Mailer 在邀请落库后同步提交邮件，不改变邀请状态或成员资格。
type Mailer struct {
	db         mailStore
	logger     *slog.Logger
	from       string
	consoleURL string
	sender     interface {
		SendMessage(context.Context, string, []byte) error
	}
}

func NewMailer(cfg config.AuthConfig, database mailStore, logger *slog.Logger) *Mailer {
	m := &Mailer{db: database, logger: logging.LoggerOrDefault(logger), from: cfg.SMTP.Username, consoleURL: cfg.ConsoleURL}
	if cfg.SMTP.Addr != "" && cfg.ConsoleURL != "" {
		m.sender = platformauth.NewMailSender(cfg.SMTP)
	}
	return m
}

// Notify 的 sent 仅表示 SMTP 接受，不承诺收件箱投递。失败可通过重发恢复。
func (m *Mailer) Notify(ctx context.Context, principal auth.Principal, recipient string, expiresAt time.Time) string {
	if m == nil || m.sender == nil {
		return "not_configured"
	}
	org, err := m.db.GetPlatformOrganization(ctx, principal.OrganizationUUID)
	if err != nil || org == nil {
		return "failed"
	}
	inviter := "组织管理员"
	if principal.UserExternalID != "" {
		user, err := m.db.GetAdminUser(ctx, principal.OrganizationUUID, principal.UserExternalID)
		if err != nil {
			return "failed"
		}
		inviter = user.Email
	}
	message, err := invitationMessage(m.from, recipient, org.Name, inviter, strings.TrimSuffix(m.consoleURL, "/")+"/invites", expiresAt)
	if err == nil {
		err = m.sender.SendMessage(ctx, recipient, message)
	}
	if err != nil {
		// SMTP 错误可能包含收件人地址，不将原始错误或邮件内容写入日志。
		m.logger.WarnContext(ctx, "invitation email submission failed", "organization_uuid", principal.OrganizationUUID)
		return "failed"
	}
	return "sent"
}

var invitationEmail = template.Must(template.New("invitation").Parse(`<!doctype html><html lang="zh-CN"><body style="margin:0;background:#fff;color:#171717;font-family:Arial,sans-serif"><main style="max-width:600px;margin:auto;padding:48px 24px;text-align:center"><p style="font-size:24px">Open Managed Agents</p><p aria-hidden="true" style="font-size:64px;margin:56px 0">✉</p><h1 style="font-size:28px">加入 {{.Organization}}</h1><p style="font-size:18px;line-height:1.7">{{.Inviter}} 邀请你加入 Open Managed Agents 上的 {{.Organization}}。</p><p>请在 {{.Expires}} 前接受邀请。</p><p style="margin:36px 0"><a href="{{.URL}}" style="display:inline-block;background:#171717;color:#fff;text-decoration:none;border-radius:10px;padding:18px 36px;font-weight:bold">接受邀请</a></p><p style="color:#666;font-size:14px">请使用接收此邀请的邮箱登录，再确认加入组织。打开链接不会自动接受邀请。</p><p style="font-size:12px;word-break:break-all">按钮无法打开时，请访问：<a href="{{.URL}}">{{.URL}}</a></p></main></body></html>`))

func invitationMessage(from, recipient, organization, inviter, link string, expiry time.Time) ([]byte, error) {
	var body bytes.Buffer
	err := invitationEmail.Execute(&body, struct{ Organization, Inviter, URL, Expires string }{organization, inviter, link, expiry.UTC().Format("2006-01-02 15:04 UTC")})
	if err != nil {
		return nil, err
	}
	var message bytes.Buffer
	message.WriteString("From: " + (&mail.Address{Name: "Open Managed Agents", Address: from}).String() + "\r\n")
	message.WriteString("To: " + (&mail.Address{Address: recipient}).String() + "\r\n")
	message.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", "你的 Open Managed Agents 组织邀请") + "\r\n")
	message.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	message.Write(body.Bytes())
	return message.Bytes(), nil
}

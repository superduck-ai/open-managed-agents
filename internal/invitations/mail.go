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

func (m *Mailer) Notify(ctx context.Context, principal auth.Principal, recipient string, expiresAt time.Time) string {
	if m == nil || m.sender == nil {
		return "not_configured"
	}
	org, err := m.db.GetPlatformOrganization(ctx, principal.OrganizationUUID)
	if err != nil || org == nil {
		return "failed"
	}
	inviter := "Organization administrator"
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
		m.logger.WarnContext(ctx, "invitation email submission failed", "organization_uuid", principal.OrganizationUUID)
		return "failed"
	}
	return "sent"
}

var invitationEmail = template.Must(template.New("invitation").Parse(`<!doctype html><html lang="en"><body style="margin:0;background:#fff;color:#171717;font-family:Arial,sans-serif"><main style="max-width:600px;margin:auto;padding:48px 24px;text-align:center"><p style="font-size:24px">Open Managed Agents</p><p aria-hidden="true" style="font-size:64px;margin:56px 0">✉</p><h1 style="font-size:28px">Join {{.Organization}}</h1><p style="font-size:18px;line-height:1.7">{{.Inviter}} invited you to join {{.Organization}} on Open Managed Agents.</p><p>Accept this invitation before {{.Expires}}.</p><p style="margin:36px 0"><a href="{{.URL}}" style="display:inline-block;background:#171717;color:#fff;text-decoration:none;border-radius:10px;padding:18px 36px;font-weight:bold">Accept invitation</a></p><p style="color:#666;font-size:14px">Sign in with the email address that received this invitation, then confirm joining the organization. Opening this link does not automatically accept the invitation.</p><p style="font-size:12px;word-break:break-all">If the button does not work, visit: <a href="{{.URL}}">{{.URL}}</a></p></main></body></html>`))

func invitationMessage(from, recipient, organization, inviter, link string, expiry time.Time) ([]byte, error) {
	var body bytes.Buffer
	err := invitationEmail.Execute(&body, struct{ Organization, Inviter, URL, Expires string }{organization, inviter, link, expiry.UTC().Format("2006-01-02 15:04 UTC")})
	if err != nil {
		return nil, err
	}
	var message bytes.Buffer
	message.WriteString("From: " + (&mail.Address{Name: "Open Managed Agents", Address: from}).String() + "\r\n")
	message.WriteString("To: " + (&mail.Address{Address: recipient}).String() + "\r\n")
	message.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", "Your Open Managed Agents organization invitation") + "\r\n")
	message.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	message.Write(body.Bytes())
	return message.Bytes(), nil
}

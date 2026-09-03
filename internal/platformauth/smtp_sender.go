package platformauth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const smtpTimeout = 15 * time.Second

type smtpSender struct {
	addr        string
	host        string
	username    string
	password    string
	implicitTLS bool
}

func newSMTPSender(cfg config.EmailSMTPConfig) *smtpSender {
	host, port, _ := net.SplitHostPort(cfg.Addr)
	return &smtpSender{
		addr:        cfg.Addr,
		host:        host,
		username:    cfg.Username,
		password:    cfg.Password,
		implicitTLS: port == "465",
	}
}

func (s *smtpSender) SendLoginCode(ctx context.Context, recipient, code string) error {
	to, err := mail.ParseAddress(recipient)
	if err != nil || to.Address != recipient {
		return errors.New("invalid SMTP recipient")
	}
	connection, err := (&net.Dialer{Timeout: smtpTimeout}).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("connect SMTP server: %w", err)
	}
	defer func() { _ = connection.Close() }()
	if err := connection.SetDeadline(s.deadline(ctx)); err != nil {
		return fmt.Errorf("set SMTP deadline: %w", err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.host}
	if s.implicitTLS {
		tlsConnection := tls.Client(connection, tlsConfig)
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("start implicit SMTP TLS: %w", err)
		}
		connection = tlsConnection
	}
	client, err := smtp.NewClient(connection, s.host)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if !s.implicitTLS {
		if supported, _ := client.Extension("STARTTLS"); !supported {
			return fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
		return fmt.Errorf("authenticate SMTP client: %w", err)
	}
	if err := client.Mail(s.username); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	if _, err := writer.Write(s.message(to.String(), code)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close SMTP message: %w", err)
	}
	_ = client.Quit()
	return nil
}

func (s *smtpSender) deadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(smtpTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

func (s *smtpSender) message(recipient, code string) []byte {
	subject := mime.QEncoding.Encode("UTF-8", "Open Managed Agents login verification code / 登录验证码")
	body := fmt.Sprintf(
		"Your login verification code is: %s\r\n\r\n"+
			"This code expires in %d minutes. Do not share it with anyone.\r\n"+
			"If you did not request this code, you can ignore this email.\r\n\r\n"+
			"你的登录验证码是：%s\r\n\r\n"+
			"验证码将在 %d 分钟内失效，请勿转发给任何人。\r\n"+
			"如果不是你本人操作，请忽略此邮件。\r\n",
		code,
		emailCodeTTL/time.Minute,
		code,
		emailCodeTTL/time.Minute,
	)
	return fmt.Appendf(nil,
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s",
		s.username,
		recipient,
		subject,
		time.Now().Format(time.RFC1123Z),
		body,
	)
}

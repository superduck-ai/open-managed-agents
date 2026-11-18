package platformauth

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

func TestDeliveryFailureLogsSafeMetadata(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	err := Deliver(t.Context(), logger, Delivery{Channel: "smtp", Send: func(context.Context) error {
		return errors.New("user@example.com secret 123456")
	}})
	if err == nil || strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "123456") || strings.Contains(output.String(), "@") {
		t.Fatal("failure log exposed notification contents")
	}
	var record struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil || record.Level != "WARN" || record.Message != "notification submission failed" || record.Channel != "smtp" {
		t.Fatal("missing structured submission failure metadata")
	}
}

func TestLoginCodeDelivery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		smtpFail   bool
		feishuFail bool
	}{
		{"both fail", true, true},
		{"SMTP fails", true, false},
		{"Feishu fails", false, true},
		{"both succeed", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			smtp := &recordingCodeSender{fail: tc.smtpFail, done: make(chan struct{})}
			feishu := &recordingCodeSender{fail: tc.feishuFail, done: make(chan struct{})}
			codes := &fakeEmailCodeStore{}
			sender := &loginCodeSenders{smtp: smtp, feishu: feishu, logger: logging.LoggerOrDefault(nil)}
			store := &fakePlatformAuthStore{tx: &fakePlatformAuthTx{findContext: db.PlatformAuthUserContext{UserExternalID: "user_test", OrgUUID: "org_test"}}}
			service := NewEmailProvider(store, codes, sender, []byte("test-secret"), nil)
			err := service.RequestEmailLogin(t.Context(), " User@Example.com ")
			for _, completed := range []<-chan struct{}{smtp.done, feishu.done} {
				select {
				case <-completed:
				case <-time.After(time.Second):
					t.Fatal("configured sender did not finish")
				}
			}
			if smtp.recipient != "user@example.com" || feishu.recipient != smtp.recipient || !validLoginCode(smtp.code) || smtp.code != feishu.code {
				t.Fatal("both destinations must receive the same code for the normalized email")
			}
			if tc.smtpFail && tc.feishuFail {
				if applicationErrorKind(err) != apperr.Unavailable || codes.revokedChallengeID == "" {
					t.Fatalf("failed delivery must revoke the code: %v", err)
				}
				return
			}
			if err != nil || codes.revokedChallengeID != "" {
				t.Fatalf("one accepted delivery must preserve the code: %v", err)
			}
			if _, _, err := service.VerifyEmailLogin(t.Context(), "user@example.com", smtp.code); err != nil {
				t.Fatal(err)
			}
			if err := service.CompleteEmailLogin(t.Context(), "user@example.com", smtp.code); err != nil {
				t.Fatal(err)
			}
			if _, _, err := service.VerifyEmailLogin(t.Context(), "user@example.com", smtp.code); applicationErrorKind(err) != apperr.Unauthenticated {
				t.Fatal("a code delivered twice must still be consumable only once")
			}
		})
	}
}

type recordingCodeSender struct {
	fail      bool
	recipient string
	code      string
	done      chan struct{}
}

func (s *recordingCodeSender) SendLoginCode(_ context.Context, recipient, code string) error {
	defer close(s.done)
	s.recipient, s.code = recipient, code
	if s.fail {
		return errors.New("delivery failed")
	}
	return nil
}

func TestDeliveryReturnsOnSuccessAndContinuesOtherSender(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	releaseSender := sync.OnceFunc(func() { close(release) })
	defer releaseSender()
	completed := make(chan error, 1)
	result := make(chan error, 1)
	go func() {
		result <- Deliver(ctx, logging.LoggerOrDefault(nil),
			Delivery{Channel: "smtp", Send: func(ctx context.Context) error {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
				}
				completed <- ctx.Err()
				return ctx.Err()
			}},
			Delivery{Channel: "feishu", Send: func(ctx context.Context) error {
				<-started
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 15*time.Second {
					return errors.New("notification sender has no bounded deadline")
				}
				return nil
			}},
		)
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("successful delivery waited for the slow sender")
	}
	cancel()
	releaseSender()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("request cancellation stopped the remaining sender: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("remaining sender did not finish")
	}
}

func TestNotificationsSendConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	delivery := func(ctx context.Context) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	result := make(chan error, 1)
	go func() {
		result <- Deliver(ctx, logging.LoggerOrDefault(nil), Delivery{Channel: "smtp", Send: delivery}, Delivery{Channel: "feishu", Send: delivery})
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("one sender blocked the other")
		}
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestFeishuOnlyRequiresVerifiedCode(t *testing.T) {
	service := New(config.AuthConfig{Feishu: config.FeishuConfig{AppID: "cli_test", AppSecret: "secret"}}, nil, nil, nil)
	if !service.VerifiedLoginEnabled() {
		t.Fatal("Feishu-only deployment must enable real authentication")
	}
	if _, _, err := service.VerifyEmailLogin(t.Context(), "user@example.com", "anything"); applicationErrorKind(err) != apperr.Unauthenticated {
		t.Fatal("Feishu-only deployment accepted a development code")
	}
}

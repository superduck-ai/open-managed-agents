package platformauth

import (
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestServiceEmailLogin(t *testing.T) {
	t.Run("failure invalid email", func(t *testing.T) {
		service, _, _ := newEmailLoginTestService(&fakePlatformAuthStore{})
		if err := service.RequestEmailLogin(t.Context(), "not-an-email"); applicationErrorKind(err) != apperr.InvalidArgument {
			t.Fatalf("RequestEmailLogin() error = %v, want invalid argument", err)
		}
	})

	t.Run("failure rate limited", func(t *testing.T) {
		service, codes, _ := newEmailLoginTestService(&fakePlatformAuthStore{})
		codes.issueErr = errEmailLoginRateLimited
		if err := service.RequestEmailLogin(t.Context(), "user@example.com"); applicationErrorKind(err) != apperr.RateLimited {
			t.Fatalf("RequestEmailLogin() error = %v, want rate limited", err)
		}
	})

	t.Run("failure sender revokes challenge", func(t *testing.T) {
		service, codes, sender := newEmailLoginTestService(&fakePlatformAuthStore{})
		sender.err = errors.New("SMTP unavailable")
		if err := service.RequestEmailLogin(t.Context(), "user@example.com"); applicationErrorKind(err) != apperr.Unavailable {
			t.Fatalf("RequestEmailLogin() error = %v, want unavailable", err)
		}
		if codes.issue == nil || codes.revokedChallengeID != codes.issue.ChallengeID {
			t.Fatalf("revoked challenge = %q, want issued challenge", codes.revokedChallengeID)
		}
		if !codes.revokeHasDeadline {
			t.Fatal("Revoke() context has no deadline")
		}
	})

	t.Run("fallback accepts any non-empty code when SMTP is omitted", func(t *testing.T) {
		tx := &fakePlatformAuthTx{findContext: db.PlatformAuthUserContext{UserExternalID: "user_existing", OrgUUID: "org-existing"}}
		service := New(config.AuthConfig{}, &fakePlatformAuthStore{tx: tx}, nil, nil)
		if err := service.RequestEmailLogin(t.Context(), "user@example.com"); err != nil {
			t.Fatalf("RequestEmailLogin() error = %v", err)
		}
		userID, orgUUID, err := service.VerifyEmailLogin(t.Context(), "user@example.com", "anything")
		if err != nil || userID != "user_existing" || orgUUID != "org-existing" {
			t.Fatalf("VerifyEmailLogin() = (%q, %q, %v), want fake-code login", userID, orgUUID, err)
		}
		if _, _, err := service.VerifyEmailLogin(t.Context(), "user@example.com", ""); applicationErrorKind(err) != apperr.Unauthenticated {
			t.Fatalf("VerifyEmailLogin(empty code) error = %v, want unauthenticated", err)
		}
		if err := service.CompleteEmailLogin(t.Context(), "user@example.com", "anything"); err != nil {
			t.Fatalf("CompleteEmailLogin() error = %v", err)
		}
	})

	t.Run("failure provisioning leaves verified code retryable", func(t *testing.T) {
		findErr := errors.New("database unavailable")
		tx := &fakePlatformAuthTx{findErr: findErr}
		service, _, sender := newEmailLoginTestService(&fakePlatformAuthStore{tx: tx})
		if err := service.RequestEmailLogin(t.Context(), "user@example.com"); err != nil {
			t.Fatalf("RequestEmailLogin() error = %v", err)
		}
		if _, _, err := service.VerifyEmailLogin(t.Context(), "user@example.com", sender.code); applicationErrorKind(err) != apperr.Unavailable {
			t.Fatalf("VerifyEmailLogin() error = %v, want unavailable", err)
		}
		tx.findErr = nil
		tx.findContext = db.PlatformAuthUserContext{UserExternalID: "user_existing", OrgUUID: "org-existing"}
		userID, orgUUID, err := service.VerifyEmailLogin(t.Context(), "user@example.com", sender.code)
		if err != nil || userID != "user_existing" || orgUUID != "org-existing" {
			t.Fatalf("VerifyEmailLogin(retry) = (%q, %q, %v), want same code accepted", userID, orgUUID, err)
		}
	})

	t.Run("failure wrong and reused code cannot provision", func(t *testing.T) {
		tx := &fakePlatformAuthTx{findContext: db.PlatformAuthUserContext{UserExternalID: "user_existing", OrgUUID: "org-existing"}}
		service, _, sender := newEmailLoginTestService(&fakePlatformAuthStore{tx: tx})
		if err := service.RequestEmailLogin(t.Context(), "Ada@example.com"); err != nil {
			t.Fatalf("RequestEmailLogin() error = %v", err)
		}
		if _, _, err := service.VerifyEmailLogin(t.Context(), "ada@example.com", "000000"); applicationErrorKind(err) != apperr.Unauthenticated {
			t.Fatalf("VerifyEmailLogin(wrong) error = %v, want unauthenticated", err)
		}
		userID, orgUUID, err := service.VerifyEmailLogin(t.Context(), "ada@example.com", sender.code)
		if err != nil || userID != "user_existing" || orgUUID != "org-existing" {
			t.Fatalf("VerifyEmailLogin() = (%q, %q, %v), want existing user", userID, orgUUID, err)
		}
		if err := service.CompleteEmailLogin(t.Context(), "ada@example.com", sender.code); err != nil {
			t.Fatalf("CompleteEmailLogin() error = %v", err)
		}
		if _, _, err := service.VerifyEmailLogin(t.Context(), "ada@example.com", sender.code); applicationErrorKind(err) != apperr.Unauthenticated {
			t.Fatalf("VerifyEmailLogin(reused) error = %v, want unauthenticated", err)
		}
	})

	t.Run("success sends six digit code", func(t *testing.T) {
		service, codes, sender := newEmailLoginTestService(&fakePlatformAuthStore{})
		if err := service.RequestEmailLogin(t.Context(), " User@Example.com "); err != nil {
			t.Fatalf("RequestEmailLogin() error = %v", err)
		}
		if sender.recipient != "user@example.com" || !validLoginCode(sender.code) {
			t.Fatalf("sent login code = recipient %q code %q", sender.recipient, sender.code)
		}
		if codes.issue == nil || codes.issue.Digest == "" || codes.issue.Digest == sender.code {
			t.Fatalf("stored issue = %#v, want non-plaintext digest", codes.issue)
		}
	})
}

func applicationErrorKind(err error) apperr.Kind {
	appErr, _ := errors.AsType[*apperr.Error](err)
	if appErr == nil {
		return 0
	}
	return appErr.Kind
}

func newEmailLoginTestService(store Store) (*EmailProvider, *fakeEmailCodeStore, *fakeLoginCodeSender) {
	codes := &fakeEmailCodeStore{}
	sender := &fakeLoginCodeSender{}
	return NewEmailProvider(store, codes, sender, []byte("01234567890123456789012345678901"), nil), codes, sender
}

type fakeEmailCodeStore struct {
	issue              *EmailCodeIssue
	issueErr           error
	used               bool
	revokedChallengeID string
	revokeHasDeadline  bool
}

func (s *fakeEmailCodeStore) Issue(_ context.Context, issue EmailCodeIssue) error {
	if s.issueErr != nil {
		return s.issueErr
	}
	s.issue = new(issue)
	return nil
}

func (s *fakeEmailCodeStore) Check(_ context.Context, emailHash, digest string) error {
	if s.issue == nil || s.used || s.issue.EmailHash != emailHash || s.issue.Digest != digest {
		return errEmailLoginCodeInvalid
	}
	return nil
}

func (s *fakeEmailCodeStore) Consume(_ context.Context, emailHash, digest string) error {
	if err := s.Check(context.Background(), emailHash, digest); err != nil {
		return err
	}
	s.used = true
	return nil
}

func (s *fakeEmailCodeStore) Revoke(ctx context.Context, _ string, challengeID string) error {
	s.revokedChallengeID = challengeID
	_, s.revokeHasDeadline = ctx.Deadline()
	return nil
}

type fakeLoginCodeSender struct {
	recipient string
	code      string
	err       error
}

func (s *fakeLoginCodeSender) SendLoginCode(_ context.Context, recipient, code string) error {
	if s.err != nil {
		return s.err
	}
	s.recipient = recipient
	s.code = code
	return nil
}

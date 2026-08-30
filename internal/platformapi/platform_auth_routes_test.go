package platformapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

type emailLoginServiceStub struct {
	requestErr     error
	verifyErr      error
	requestedEmail string
}

func (s *emailLoginServiceStub) RequestEmailLogin(_ context.Context, email string) error {
	s.requestedEmail = email
	return s.requestErr
}

func (s *emailLoginServiceStub) VerifyEmailLogin(context.Context, string, string) (string, string, error) {
	return "", "", s.verifyErr
}

type emailLoginStoreStub struct{}

func (emailLoginStoreStub) ResolvePlatformSessionIdentity(context.Context, platformsession.CreateInput) (platformsession.Session, error) {
	return platformsession.Session{}, errors.New("unexpected session resolution")
}

func (emailLoginStoreStub) FindBootstrapUserContext(context.Context, string) (string, string, error) {
	return "", "", errors.New("unexpected bootstrap lookup")
}

func (emailLoginStoreStub) GetBootstrapUser(context.Context, string) (*UserRecord, error) {
	return nil, errors.New("unexpected bootstrap lookup")
}

func (emailLoginStoreStub) ListBootstrapUserOrganizations(context.Context, string, string) ([]UserOrganizationRecord, error) {
	return nil, errors.New("unexpected bootstrap lookup")
}

func TestSendMagicLinkHTTPContract(t *testing.T) {
	t.Run("malformed request", func(t *testing.T) {
		service := &emailLoginServiceStub{}
		response := serveEmailLoginRequest(handleSendMagicLink(service), `{`)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
		}
	})

	t.Run("accepted", func(t *testing.T) {
		service := &emailLoginServiceStub{}
		response := serveEmailLoginRequest(handleSendMagicLink(service), `{"email_address":"user@example.com"}`)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"sent":true`) {
			t.Fatalf("response = %d %s, want sent response", response.Code, response.Body.String())
		}
		if service.requestedEmail != "user@example.com" {
			t.Fatalf("requested email = %q", service.requestedEmail)
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		service := &emailLoginServiceStub{requestErr: apperr.New(apperr.RateLimited, "Too many attempts. Try again later", errors.New("limited"))}
		response := serveEmailLoginRequest(handleSendMagicLink(service), `{"email_address":"user@example.com"}`)
		if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "" {
			t.Fatalf("response = %d Retry-After=%q, want 429 without a guessed retry delay", response.Code, response.Header().Get("Retry-After"))
		}
	})
}

func TestVerifyMagicLinkInvalidCodeReturnsUnauthorized(t *testing.T) {
	service := &emailLoginServiceStub{verifyErr: apperr.New(apperr.Unauthenticated, "Verification code is invalid or expired", errors.New("invalid"))}
	handler := handleVerifyMagicLink(emailLoginStoreStub{}, service, platformsession.NewMemoryStore(), false)
	response := serveEmailLoginRequest(handler, `{"credentials":{"email_address":"user@example.com","code":"000000"}}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func serveEmailLoginRequest(handler http.Handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/auth/send_magic_link", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

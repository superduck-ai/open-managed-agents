package platformauth

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

type feishuDeliveryError struct {
	httpStatus int
	apiCode    *int
}

func (e *feishuDeliveryError) Error() string {
	message := fmt.Sprintf("feishu HTTP status %d", e.httpStatus)
	if e.apiCode != nil {
		message += fmt.Sprintf(", API error %d", *e.apiCode)
	}
	return message
}

func (e *feishuDeliveryError) LogValue() slog.Value {
	attributes := []slog.Attr{slog.Int("http_status", e.httpStatus)}
	if e.apiCode != nil {
		attributes = append(attributes, slog.Int("api_code", *e.apiCode))
	}
	return slog.GroupValue(attributes...)
}

var (
	errEmailLoginRateLimited = errors.New("email login rate limited")
	errEmailLoginCodeInvalid = errors.New("email login code invalid")
	errNotificationDelivery  = errors.New("notification delivery failed")
)

func invalidEmail(cause error) error {
	return apperr.New(apperr.InvalidArgument, "Enter a valid email address", cause)
}

func invalidLoginCode(cause error) error {
	return apperr.New(apperr.Unauthenticated, "Verification code is invalid or expired", cause)
}

func emailLoginRateLimited(cause error) error {
	return apperr.New(apperr.RateLimited, "Too many attempts. Try again later", cause)
}

func emailLoginUnavailable(message string, cause error) error {
	return apperr.New(apperr.Unavailable, message, cause)
}

func emailLoginDomainDenied() error {
	return apperr.New(apperr.PermissionDenied, "Use an approved email address to sign in", nil)
}

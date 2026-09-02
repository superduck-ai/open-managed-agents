package platformauth

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

var (
	errEmailLoginRateLimited = errors.New("email login rate limited")
	errEmailLoginCodeInvalid = errors.New("email login code invalid")
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

package admin

import (
	"errors"
	"net/mail"
	"strings"
)

var (
	errInvalidEmail = errors.New("invalid email")
)

func validateOrganizationRole(role string) error {
	if role == "user" || role == "admin" {
		return nil
	}
	return errors.New("invalid organization role")
}

func normalizeEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", errInvalidEmail
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Address == "" {
		return "", errInvalidEmail
	}
	if parsed.Name != "" && parsed.String() != parsed.Address {
		return "", errInvalidEmail
	}
	return strings.ToLower(parsed.Address), nil
}

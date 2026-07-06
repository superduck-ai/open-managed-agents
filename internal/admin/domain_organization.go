package admin

import (
	"errors"
	"net/mail"
	"strings"
)

var (
	errInvalidEmail = errors.New("invalid email")
)

func validateOrganizationRole(role string, allowAdmin bool) error {
	switch role {
	case "user", "developer", "billing", "claude_code_user":
		return nil
	case "admin":
		if allowAdmin {
			return nil
		}
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

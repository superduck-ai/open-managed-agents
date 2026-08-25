package db

import (
	"fmt"
	"strings"
	"uuid"
)

func tryParseDBUUIDIdentifierString(value string) string {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil() {
		return ""
	}
	return parsed.String()
}

func parseDBUUID(name, value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil() {
		return uuid.Nil(), fmt.Errorf("%s must be a non-nil UUID", name)
	}
	return parsed, nil
}

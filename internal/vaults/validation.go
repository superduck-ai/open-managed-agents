package vaults

import (
	"fmt"
	"strings"
)

func requireNonEmptyString(value, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return trimmed, nil
}

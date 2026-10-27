package vaults

import (
	"fmt"
	"strings"
)

// requireNonEmptyString rejects blank-after-trim identifiers and returns the trimmed value.
func requireNonEmptyString(value, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return trimmed, nil
}

// requireNonBlankVerbatim rejects blank-after-trim payloads but returns value unchanged so
// intentional leading/trailing whitespace (for example env secret_value) is preserved.
func requireNonBlankVerbatim(value, name string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}

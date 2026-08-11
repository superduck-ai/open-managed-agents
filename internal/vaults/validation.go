package vaults

import (
	"fmt"
	"strings"
)

func requireNonEmptyString(value, name string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}

package secrets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ResolveKEK returns the 32-byte KEK from a base64 kek value or a kek file.
// The file holds the same base64 text as the inline value (trimmed). Exactly one
// source may be configured; configuring both or neither is an error so callers
// fail fast at startup instead of silently falling back.
func ResolveKEK(base64KEK, kekFile string) ([]byte, error) {
	base64KEK = strings.TrimSpace(base64KEK)
	kekFile = strings.TrimSpace(kekFile)
	switch {
	case base64KEK != "" && kekFile != "":
		return nil, errors.New("secrets: configure at most one of vault.master_key.kek or kek_file")
	case base64KEK != "":
		return decodeKEK(base64KEK, "vault.master_key.kek")
	case kekFile != "":
		data, err := os.ReadFile(kekFile)
		if err != nil {
			return nil, fmt.Errorf("secrets: read vault.master_key.kek_file: %w", err)
		}
		return decodeKEK(string(data), "vault.master_key.kek_file")
	default:
		return nil, errors.New("secrets: vault.master_key.kek or kek_file is required")
	}
}

func decodeKEK(value, source string) ([]byte, error) {
	kek, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("secrets: decode %s: %w", source, err)
	}
	if len(kek) != 32 {
		return nil, fmt.Errorf("secrets: %s must decode to 32 bytes, got %d", source, len(kek))
	}
	return kek, nil
}

// GenerateKEK returns a fresh random 32-byte KEK for tests and offline key
// generation helpers. Production and local servers must load a configured KEK
// from vault.master_key; there is no process-scoped ephemeral startup fallback.
func GenerateKEK() ([]byte, error) {
	kek, err := randomBytes(32)
	if err != nil {
		return nil, fmt.Errorf("secrets: generate KEK: %w", err)
	}
	return kek, nil
}

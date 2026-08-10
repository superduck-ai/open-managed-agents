package vaults

import (
	"bytes"
)

// isJSONNull reports whether raw is the JSON null literal (possibly
// surrounded by whitespace). Empty input also counts as null.
func isJSONNull(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

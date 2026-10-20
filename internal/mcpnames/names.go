package mcpnames

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf8"
)

var legacyName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func Valid(name string) bool {
	return utf8.ValidString(name) && utf8.RuneCountInString(name) >= 1 && utf8.RuneCountInString(name) <= 255
}

func ServerKey(name string) string {
	if legacyName.MatchString(name) {
		return name
	}
	digest := sha256.Sum256([]byte(name))
	return "mcp_" + hex.EncodeToString(digest[:12])
}

func ToolPrefix(name string) string {
	return strings.ReplaceAll(ServerKey(name), ".", "_")
}

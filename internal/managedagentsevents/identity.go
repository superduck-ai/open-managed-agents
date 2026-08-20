package managedagentsevents

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

func StableAssistantEventID(codeSessionID, messageID string, contentBlockIndex int, eventType string) string {
	seed := "assistant-preview-v1\x00" + strings.TrimSpace(messageID) + "\x00" + strconv.Itoa(contentBlockIndex) + "\x00" + strings.TrimSpace(eventType)
	sum := sha256.Sum256([]byte(strings.TrimSpace(codeSessionID) + "\x00public\x00" + seed))
	return "sevt_" + hex.EncodeToString(sum[:16])
}

func ClaudeTaskThreadID(codeSessionID, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(codeSessionID) + "\x00claude-task\x00" + key))
	return "sthr_" + hex.EncodeToString(sum[:16])
}

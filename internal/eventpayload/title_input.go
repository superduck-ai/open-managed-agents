package eventpayload

import (
	"encoding/json/v2"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/titletext"
)

func sessionTitleInput(event db.SessionEvent) string {
	const maxTitleInputBytes = 4096
	if event.EventType != "user.message" {
		return ""
	}
	var message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(event.Payload, &message) != nil {
		return ""
	}
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type != "text" {
			continue
		}
		content := titletext.Normalize(block.Text)
		if content == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte(' ')
		}
		text.WriteString(content)
		if text.Len() >= maxTitleInputBytes {
			break
		}
	}
	return titletext.Clip(text.String(), maxTitleInputBytes)
}

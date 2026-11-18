package eventpayload

import (
	"encoding/json/v2"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/titletext"
)

func sessionTitleInput(event db.SessionEvent) string {
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
		if block.Type == "text" {
			text.WriteString(block.Text)
			text.WriteByte('\n')
		}
		if text.Len() >= 4096 {
			break
		}
	}
	return titletext.Normalize(text.String(), 4096)
}

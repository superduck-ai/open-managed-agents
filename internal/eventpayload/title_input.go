package eventpayload

import (
	"context"
	"encoding/json/v2"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/titletext"
)

func (s *Store) FirstSessionTitleInput(ctx context.Context, session db.Session) (string, error) {
	var afterUUID string
	for {
		events, err := s.database.ListSessionTitleInputs(ctx, session.WorkspaceUUID, session.UUID, afterUUID)
		if err != nil || len(events) == 0 {
			return "", err
		}
		for _, event := range events {
			event.Payload, err = s.restore(ctx, event.WorkspaceUUID, event.Payload, event.PayloadBlobUUID)
			if err != nil {
				return "", err
			}
			if text := sessionTitleInput(event); text != "" {
				return text, nil
			}
		}
		afterUUID = events[len(events)-1].UUID
	}
}

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

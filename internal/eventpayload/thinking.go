package eventpayload

import (
	jsonv2 "encoding/json/v2"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func withoutThinkingContent(events []db.SessionEvent) ([]db.SessionEvent, error) {
	stripped := make([]db.SessionEvent, len(events))
	copy(stripped, events)
	for i, event := range stripped {
		if event.EventType != "agent.thinking" || len(event.Payload) == 0 {
			continue
		}
		var payload map[string]any
		if err := jsonv2.Unmarshal(event.Payload, &payload); err != nil {
			return nil, err
		}
		_, hasContent := payload["content"]
		_, hasMessage := payload["message"]
		if !hasContent && !hasMessage {
			continue
		}
		delete(payload, "content")
		delete(payload, "message")
		encoded, err := jsonv2.Marshal(payload)
		if err != nil {
			return nil, err
		}
		stripped[i].Payload = encoded
	}
	return stripped, nil
}

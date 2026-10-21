package managedagentsevents

import "encoding/json"

func IsThinkingDelta(raw json.RawMessage) bool {
	var payload struct {
		Type  string `json:"type"`
		Delta struct {
			Type    string `json:"type"`
			Content struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"delta"`
	}
	return json.Unmarshal(raw, &payload) == nil && payload.Type == "event_delta" && payload.Delta.Type == "content_delta" && payload.Delta.Content.Type == "thinking"
}

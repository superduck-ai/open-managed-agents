package managedagentsevents

import (
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
)

func EqualPublicationPayload(kind string, left, right []byte) (bool, error) {
	if kind != "agent.message" && kind != "agent.thinking" {
		return jsoncanonical.Equal(left, right)
	}
	var a, b struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(left, &a); err != nil {
		return false, err
	}
	if err := json.Unmarshal(right, &b); err != nil {
		return false, err
	}
	if kind == "agent.thinking" {
		return true, nil
	}
	if len(a.Content) == 0 || len(b.Content) == 0 {
		return len(a.Content) == len(b.Content), nil
	}
	return jsoncanonical.Equal(a.Content, b.Content)
}

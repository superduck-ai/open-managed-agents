package agents

import (
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type modelEffort struct {
	Type string `json:"type"`
}

func normalizeModelEffort(raw json.RawMessage) (*modelEffort, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var value modelEffort
	if json.Unmarshal(raw, &value.Type) != nil {
		if json.Unmarshal(raw, &value) != nil {
			return nil, errInvalidModelEffort
		}
	}
	switch value.Type {
	case "low", "medium", "high", "xhigh", "max":
		return &value, nil
	default:
		return nil, errInvalidModelEffort
	}
}

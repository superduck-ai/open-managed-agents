package agents

import "encoding/json"

func NormalizeSessionModelOverride(raw json.RawMessage) (json.RawMessage, error) {
	model, err := normalizeModel(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(model)
}

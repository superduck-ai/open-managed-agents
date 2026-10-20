package agents

import "encoding/json"

func NormalizeSessionModelOverride(raw json.RawMessage, canonicalModelID func(string) (string, error)) (json.RawMessage, error) {
	model, err := normalizeModel(raw)
	if err != nil {
		return nil, err
	}
	if model.ID, err = canonicalModelID(model.ID); err != nil {
		return nil, err
	}
	return json.Marshal(model)
}

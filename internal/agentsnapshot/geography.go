package agentsnapshot

import "encoding/json"

func inferenceGeo(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return "", nil
	}
	var model struct {
		InferenceGeo string `json:"inference_geo"`
	}
	if err := json.Unmarshal(raw, &model); err != nil {
		return "", err
	}
	return model.InferenceGeo, nil
}

func validateRosterGeography(parentModel json.RawMessage, entries []json.RawMessage) error {
	if len(entries) == 0 {
		return nil
	}
	parent, err := inferenceGeo(parentModel)
	if err != nil {
		return err
	}
	for _, raw := range entries {
		var child struct {
			Model json.RawMessage `json:"model"`
		}
		if err := json.Unmarshal(raw, &child); err != nil {
			return err
		}
		geo, err := inferenceGeo(child.Model)
		if err != nil {
			return err
		}
		if parent != geo {
			return errRosterInferenceGeoMismatch
		}
	}
	return nil
}

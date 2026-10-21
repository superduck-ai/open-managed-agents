package vaults

import "encoding/json"

func normalizeVaultCreateMetadata(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return normalizeMetadata(nil)
	}
	if isJSONNull(raw) {
		return nil, invalidVaultCreateMetadata()
	}
	var values map[string]*string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, invalidVaultCreateMetadata()
	}
	metadata := make(map[string]string, len(values))
	for key, value := range values {
		if value == nil {
			return nil, invalidVaultCreateMetadata()
		}
		metadata[key] = *value
	}
	return normalizeMetadata(metadata)
}

func patchVaultMetadata(current, raw json.RawMessage) (json.RawMessage, error) {
	if isJSONNull(raw) {
		return current, nil
	}
	return patchMetadata(current, raw)
}

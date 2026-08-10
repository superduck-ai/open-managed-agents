package vaults

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func objectFromRaw(raw json.RawMessage, name string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s is required", name)
	}
	if isJSONNull(raw) {
		return nil, fmt.Errorf("%s cannot be null", name)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	if fields == nil {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	return fields, nil
}

func requiredString(fields map[string]json.RawMessage, key, name string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return rawString(raw, name)
}

func optionalString(fields map[string]json.RawMessage, key, name string) (string, bool, error) {
	raw, ok := fields[key]
	if !ok || isJSONNull(raw) {
		return "", false, nil
	}
	value, err := rawString(raw, name)
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func rawString(raw json.RawMessage, name string) (string, error) {
	if isJSONNull(raw) {
		return "", fmt.Errorf("%s cannot be null", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}

func rawStringOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func stringArray(raw json.RawMessage, name string) ([]string, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s entries must be non-empty strings", name)
		}
		if len(value) > 253 {
			return nil, fmt.Errorf("%s entries must be at most 253 characters", name)
		}
	}
	return values, nil
}

func rawObjectMap(raw json.RawMessage) map[string]any {
	var value map[string]any
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{}
	}
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return map[string]any{}
	}
	return value
}

func nestedMap(parent map[string]any, key string) map[string]any {
	value, ok := parent[key]
	if !ok || value == nil {
		return nil
	}
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var mapped map[string]any
	if err := json.Unmarshal(raw, &mapped); err != nil {
		return nil
	}
	return mapped
}

func fieldOrDefault(fields map[string]json.RawMessage, name, fallback string) json.RawMessage {
	if raw, ok := fields[name]; ok {
		return raw
	}
	return json.RawMessage(fallback)
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func marshalRaw(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func validateHTTPURL(value, name string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid URL", name)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https", name)
	}
	return nil
}

func validateRFC3339(value, name string) error {
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("%s must be RFC3339", name)
	}
	return nil
}

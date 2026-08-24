// Package jsonx helps convert between Go values and json.RawMessage.
package jsonx

import (
	"encoding/json"
	"strings"
)

func Encode(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func Decode[T any](raw json.RawMessage) (T, error) {
	var value T
	err := json.Unmarshal(raw, &value)
	return value, err
}

func IsNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func Default(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

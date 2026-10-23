package sessioncontract

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

const (
	SandboxTimeoutMetadataKey = "timeout_seconds"
	SandboxEnvMetadataKey     = "env"
	maxSandboxTimeoutSeconds  = 86400
)

func SandboxTimeout(metadata json.RawMessage) time.Duration {
	raw, ok := metadataField(metadata, SandboxTimeoutMetadataKey)
	if !ok {
		return 0
	}
	seconds := intValue(raw)
	if seconds < 1 || seconds > maxSandboxTimeoutSeconds {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func SandboxEnv(metadata json.RawMessage) map[string]string {
	raw, ok := metadataField(metadata, SandboxEnvMetadataKey)
	if !ok || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	values := stringMap(raw)
	if len(values) > 0 {
		return values
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) != nil || encoded == "" {
		return nil
	}
	values = stringMap(json.RawMessage(encoded))
	if len(values) == 0 {
		return nil
	}
	return values
}

func metadataField(metadata json.RawMessage, key string) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(metadata)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(trimmed, &fields) != nil {
		return nil, false
	}
	raw, ok := fields[key]
	return raw, ok
}

func intValue(raw json.RawMessage) int {
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return 0
	}
	number, err := strconv.Atoi(text)
	if err != nil {
		return 0
	}
	return number
}

func stringMap(raw json.RawMessage) map[string]string {
	var values map[string]string
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return nil
	}
	return values
}

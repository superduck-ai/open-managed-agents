package qoder

import (
	"bytes"
	"encoding/json"
	"strings"
)

const (
	qoderEventPrefix   = "evt_"
	omaEventPrefix     = "sevt_qdr_"
	qoderResourcePrefx = "sesr_"
	omaResourcePrefix  = "sesrsc_qdr_"
)

func EncodeEventID(remoteID string) string {
	if remoteID == "" {
		return ""
	}
	if strings.HasPrefix(remoteID, omaEventPrefix) {
		return remoteID
	}
	if strings.HasPrefix(remoteID, qoderEventPrefix) {
		return omaEventPrefix + strings.TrimPrefix(remoteID, qoderEventPrefix)
	}
	return remoteID
}

func DecodeEventID(omaID string) string {
	if omaID == "" {
		return ""
	}
	if strings.HasPrefix(omaID, omaEventPrefix) {
		return qoderEventPrefix + strings.TrimPrefix(omaID, omaEventPrefix)
	}
	return omaID
}

func EncodeSessionResourceID(remoteID string) string {
	if remoteID == "" {
		return ""
	}
	if strings.HasPrefix(remoteID, omaResourcePrefix) {
		return remoteID
	}
	if strings.HasPrefix(remoteID, qoderResourcePrefx) {
		return omaResourcePrefix + strings.TrimPrefix(remoteID, qoderResourcePrefx)
	}
	return remoteID
}

func DecodeSessionResourceID(omaID string) string {
	if omaID == "" {
		return ""
	}
	if strings.HasPrefix(omaID, omaResourcePrefix) {
		return qoderResourcePrefx + strings.TrimPrefix(omaID, omaResourcePrefix)
	}
	return omaID
}

func EncodeEventPayload(raw json.RawMessage) (json.RawMessage, error) {
	return transformEventJSON(raw, EncodeEventID)
}

func DecodeEventPayload(raw json.RawMessage) (json.RawMessage, error) {
	return transformEventJSON(raw, DecodeEventID)
}

var eventIDStringFields = map[string]struct{}{
	"id":                     {},
	"event_id":               {},
	"tool_use_id":            {},
	"mcp_tool_use_id":        {},
	"custom_tool_use_id":     {},
	"model_request_start_id": {},
	"before_id":              {},
	"after_id":               {},
	"first_id":               {},
	"last_id":                {},
}

var eventIDNestedObjects = map[string]struct{}{
	"event":           {},
	"stop_reason":     {},
	"requires_action": {},
}

func transformEventJSON(raw json.RawMessage, fn func(string) string) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw, nil
	}
	if trimmed[0] != '{' {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, err
	}
	changed, err := transformEventObject(fields, fn)
	if err != nil {
		return nil, err
	}
	if !changed {
		return raw, nil
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func transformEventObject(fields map[string]json.RawMessage, fn func(string) string) (bool, error) {
	changed := false
	for key, val := range fields {
		if _, ok := eventIDStringFields[key]; ok {
			next, updated, err := transformJSONString(val, fn)
			if err != nil {
				return false, err
			}
			if updated {
				fields[key] = next
				changed = true
			}
			continue
		}
		if key == "event_ids" {
			next, updated, err := transformJSONStringSlice(val, fn)
			if err != nil {
				return false, err
			}
			if updated {
				fields[key] = next
				changed = true
			}
			continue
		}
		if _, ok := eventIDNestedObjects[key]; ok {
			trimmed := bytes.TrimSpace(val)
			if len(trimmed) == 0 || trimmed[0] != '{' {
				continue
			}
			var nested map[string]json.RawMessage
			if err := json.Unmarshal(trimmed, &nested); err != nil {
				return false, err
			}
			nestedChanged, err := transformEventObject(nested, fn)
			if err != nil {
				return false, err
			}
			if nestedChanged {
				encoded, err := json.Marshal(nested)
				if err != nil {
					return false, err
				}
				fields[key] = json.RawMessage(encoded)
				changed = true
			}
		}
	}
	return changed, nil
}

func transformJSONString(raw json.RawMessage, fn func(string) string) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '"' {
		return raw, false, nil
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return raw, false, err
	}
	converted := fn(value)
	if converted == value {
		return raw, false, nil
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		return raw, false, err
	}
	return json.RawMessage(encoded), true, nil
}

func transformJSONStringSlice(raw json.RawMessage, fn func(string) string) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '[' {
		return raw, false, nil
	}
	var values []string
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return raw, false, err
	}
	changed := false
	for i, v := range values {
		converted := fn(v)
		if converted != v {
			values[i] = converted
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return raw, false, err
	}
	return json.RawMessage(encoded), true, nil
}

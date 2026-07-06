package codesessions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrProtocol = errors.New("code session protocol error")

type EventMetadata struct {
	EventType      string
	EventSubtype   string
	PayloadUUID    *string
	RequestID      *string
	Payload        json.RawMessage
	PayloadHash    string
	IdempotencyKey string
}

func BuildEventMetadata(codeSessionID, direction string, raw json.RawMessage) (EventMetadata, error) {
	normalized, object, err := normalizeJSONObject(raw)
	if err != nil {
		return EventMetadata{}, err
	}
	eventType := stringField(object, "type")
	if eventType == "" {
		return EventMetadata{}, fmt.Errorf("%w: missing event type", ErrProtocol)
	}
	eventSubtype := stringField(object, "subtype")
	if eventSubtype == "" {
		eventSubtype = nestedStringField(object, "request", "subtype")
	}
	if eventSubtype == "" {
		eventSubtype = nestedStringField(object, "response", "subtype")
	}
	if eventSubtype == "" {
		eventSubtype = nestedStringField(object, "event", "type")
	}

	var payloadUUID *string
	if value := stringField(object, "uuid"); value != "" {
		payloadUUID = &value
	}
	var requestID *string
	if value := stringField(object, "request_id"); value != "" {
		requestID = &value
	}
	if requestID == nil {
		if value := nestedStringField(object, "response", "request_id"); value != "" {
			requestID = &value
		}
	}

	sum := sha256.Sum256(normalized)
	meta := EventMetadata{
		EventType:    eventType,
		EventSubtype: eventSubtype,
		PayloadUUID:  payloadUUID,
		RequestID:    requestID,
		Payload:      normalized,
		PayloadHash:  hex.EncodeToString(sum[:]),
	}
	meta.IdempotencyKey = eventIdempotencyKey(codeSessionID, direction, meta)
	return meta, nil
}

func eventIdempotencyKey(codeSessionID, direction string, meta EventMetadata) string {
	prefix := strings.TrimSpace(codeSessionID) + ":" + strings.TrimSpace(direction) + ":"
	if meta.PayloadUUID != nil && strings.TrimSpace(*meta.PayloadUUID) != "" {
		return prefix + "uuid:" + strings.TrimSpace(*meta.PayloadUUID)
	}
	if meta.RequestID != nil && strings.TrimSpace(*meta.RequestID) != "" {
		return prefix + meta.EventType + ":" + strings.TrimSpace(*meta.RequestID) + ":" + meta.EventSubtype
	}
	return prefix + "hash:" + meta.EventType + ":" + meta.PayloadHash
}

func normalizeJSONObject(raw json.RawMessage) (json.RawMessage, map[string]any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("%w: empty payload", ErrProtocol)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, nil, fmt.Errorf("%w: invalid json", ErrProtocol)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("%w: trailing json data", ErrProtocol)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%w: payload must be a json object", ErrProtocol)
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, nil, err
	}
	return json.RawMessage(encoded), object, nil
}

func decodeJSONObject(raw json.RawMessage) (map[string]any, error) {
	_, object, err := normalizeJSONObject(raw)
	return object, err
}

func stringField(object map[string]any, field string) string {
	value, ok := object[field].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func nestedStringField(object map[string]any, parent, field string) string {
	nested, ok := object[parent].(map[string]any)
	if !ok {
		return ""
	}
	return stringField(nested, field)
}

func marshalRaw(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

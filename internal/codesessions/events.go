package codesessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

type eventPayloadMetadataSchema struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	UUID      string `json:"uuid"`
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype string `json:"subtype"`
	} `json:"request"`
	Response struct {
		Subtype   string `json:"subtype"`
		RequestID string `json:"request_id"`
	} `json:"response"`
	Event struct {
		Type string `json:"type"`
	} `json:"event"`
}

func BuildEventMetadata(codeSessionID, direction string, raw json.RawMessage) (EventMetadata, error) {
	schema, err := decodeEventPayloadMetadata(raw)
	if err != nil {
		return EventMetadata{}, err
	}
	eventType := schema.Type
	if eventType == "" {
		return EventMetadata{}, fmt.Errorf("%w: missing event type", ErrProtocol)
	}
	eventSubtype := schema.Subtype
	if eventSubtype == "" {
		eventSubtype = schema.Request.Subtype
	}
	if eventSubtype == "" {
		eventSubtype = schema.Response.Subtype
	}
	if eventSubtype == "" {
		eventSubtype = schema.Event.Type
	}

	var payloadUUID *string
	if schema.UUID != "" {
		payloadUUID = &schema.UUID
	}
	var requestID *string
	if schema.RequestID != "" {
		requestID = &schema.RequestID
	}
	if requestID == nil {
		if schema.Response.RequestID != "" {
			requestID = &schema.Response.RequestID
		}
	}

	sum := sha256.Sum256(raw)
	meta := EventMetadata{
		EventType:    eventType,
		EventSubtype: eventSubtype,
		PayloadUUID:  payloadUUID,
		RequestID:    requestID,
		Payload:      raw,
		PayloadHash:  hex.EncodeToString(sum[:]),
	}
	meta.IdempotencyKey = eventIdempotencyKey(codeSessionID, direction, meta)
	return meta, nil
}

func decodeEventPayloadMetadata(raw json.RawMessage) (eventPayloadMetadataSchema, error) {
	if len(raw) == 0 {
		return eventPayloadMetadataSchema{}, fmt.Errorf("%w: empty payload", ErrProtocol)
	}
	var schema eventPayloadMetadataSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return eventPayloadMetadataSchema{}, fmt.Errorf("%w: invalid JSON: %w", ErrProtocol, err)
	}
	return schema, nil
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

func stringField(object map[string]any, field string) string {
	value, ok := object[field].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func marshalRaw(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

package codesessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func TestBuildEventMetadataRejectsInvalidPayloads(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`{"uuid":"event-uuid"}`)} {
		if _, err := BuildEventMetadata("cse_test", "outbound", raw); !errors.Is(err, ErrProtocol) {
			t.Fatalf("BuildEventMetadata(%s) error = %v, want ErrProtocol", raw, err)
		}
	}
}

func TestBuildEventMetadataRetainsRawPayloadAndDecodesSchema(t *testing.T) {
	raw := json.RawMessage(`  { "type": "control_response", "uuid": "event-uuid", "response": { "subtype": "success", "request_id": "request-id", "future": true }, "future": {"value":1} }  `)

	meta, err := BuildEventMetadata("cse_test", "outbound", raw)
	if err != nil {
		t.Fatalf("BuildEventMetadata() error = %v", err)
	}
	if string(meta.Payload) != string(raw) {
		t.Fatalf("payload = %q, want original JSON %q", meta.Payload, raw)
	}
	if meta.EventType != "control_response" || meta.EventSubtype != "success" {
		t.Fatalf("event metadata = (%q, %q)", meta.EventType, meta.EventSubtype)
	}
	if meta.PayloadUUID == nil || *meta.PayloadUUID != "event-uuid" {
		t.Fatalf("payload UUID = %v", meta.PayloadUUID)
	}
	if meta.RequestID == nil || *meta.RequestID != "request-id" {
		t.Fatalf("request ID = %v", meta.RequestID)
	}
	sum := sha256.Sum256(raw)
	if meta.PayloadHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("payload hash = %q, want hash of retained raw payload", meta.PayloadHash)
	}
}

package codesessions

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInternalPayloadRejectsInvalidTypeOrMissingUUID(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"unknown type", `{"type":"unknown","uuid":"event"}`, "unsupported internal event payload type"},
		{"missing type", `{"uuid":"event"}`, "event payload must be a json object"},
		{"missing log UUID", `{"type":"dsh_log","data":{}}`, "event payload uuid is required"},
		{"missing delivery UUID", `{"type":"ccrv2_delivery","data":{}}`, "event payload uuid is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeCodeSessionWorkerInternalEvent("cse_test", json.RawMessage(`{"payload":`+tc.payload+`}`))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestInternalPayloadPreservesNativeRecords(t *testing.T) {
	cases := []struct {
		name string
		kind string
		data string
	}{
		{"header", "dsh_log", `{ "version":1,"id":"child","createdAt":0,"isSeeded":false,"future":{"precise":9007199254740993} }`},
		{"event", "dsh_log", `{ "type":"tool/result","seq":12,"time":0,"data":{"message":{"content":[{"type":"text","text":"result\\n"}]},"future":1.234567890123456789} }`},
		{"delivery", "ccrv2_delivery", `{ "revision":1,"inputId":"input","logOffsets":{"child":2},"completed":[],"outbox":[],"future":true }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"type":"` + tc.kind + `","uuid":"event","data":` + tc.data + `,"future":"retained"}`
			raw := json.RawMessage(`{"payload":` + payload + `,"agent_id":"child","is_compaction":false}`)
			event, err := decodeCodeSessionWorkerInternalEvent("cse_test", raw)
			if err != nil {
				t.Fatal(err)
			}
			if event.EventType != tc.kind || event.PayloadUUID != "event" || event.AgentID == nil || *event.AgentID != "child" || event.IsCompaction {
				t.Fatalf("unexpected event metadata: %+v", event)
			}
			if !bytes.Equal(event.Payload, []byte(payload)) {
				t.Fatalf("payload changed: %s", event.Payload)
			}
			var stored struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(event.Payload, &stored); err != nil {
				t.Fatal(err)
			}
			if string(stored.Data) != tc.data {
				t.Fatalf("data changed: %s", stored.Data)
			}
			if strings.Contains(string(event.Payload), `"subtype"`) {
				t.Fatal("unexpected subtype")
			}
		})
	}
}

func TestInternalPayloadRetainsTranscriptTypes(t *testing.T) {
	for _, kind := range []string{"user", "assistant", "attachment", "system"} {
		t.Run(kind, func(t *testing.T) {
			raw := json.RawMessage(`{"payload":{"type":"` + kind + `","uuid":"event"}}`)
			event, err := decodeCodeSessionWorkerInternalEvent("cse_test", raw)
			if err != nil || event.EventType != kind {
				t.Fatalf("type = %q, error = %v", event.EventType, err)
			}
		})
	}
}

package codesessions

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPrepareSingleWorkerEventRejectsInvalidControlRequest(t *testing.T) {
	prepared, err := prepareSingleWorkerEvent(
		"cse_test",
		json.RawMessage(`{"type":"control_request","uuid":"control-uuid","request_id":"request-id","request":42}`),
		time.Unix(1, 0).UTC(),
	)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("prepareSingleWorkerEvent() error = %v, want ErrProtocol", err)
	}
	if prepared.eventType != "" || prepared.controlRequest != nil || len(prepared.publicPayloads) != 0 {
		t.Fatalf("prepared event = %#v, want zero value", prepared)
	}
}

func TestPrepareSingleWorkerEventBuildsKeepAliveAction(t *testing.T) {
	prepared, err := prepareSingleWorkerEvent(
		"cse_test",
		json.RawMessage(`{"type":"keep_alive"}`),
		time.Unix(1, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("prepareSingleWorkerEvent() error = %v", err)
	}
	if prepared.eventType != "keep_alive" {
		t.Fatalf("event type = %q, want keep_alive", prepared.eventType)
	}
	if prepared.controlRequest != nil || len(prepared.publicPayloads) != 0 {
		t.Fatalf("keep-alive action = %#v, want no event application", prepared)
	}
}

func TestPrepareWorkerOutputEventsRejectsBatchBeforeApply(t *testing.T) {
	events := []workerOutputEvent{
		{Payload: json.RawMessage(`{
			"type":"assistant",
			"uuid":"assistant-uuid",
			"message":{"role":"assistant","content":[{"type":"text","text":"ready"}]}
		}`)},
		{Payload: json.RawMessage(`{
			"type":"assistant",
			"uuid":"invalid-uuid",
			"response":42
		}`)},
	}

	prepared, err := prepareWorkerOutputEvents("cse_test", events, time.Unix(1, 0).UTC())
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("prepareWorkerOutputEvents() error = %v, want ErrProtocol", err)
	}
	if !strings.Contains(err.Error(), "events[1]") {
		t.Fatalf("error = %q, want failing event index", err)
	}
	if prepared != nil {
		t.Fatalf("prepared events = %#v, want nil batch", prepared)
	}
}

func TestPrepareWorkerOutputEventsBuildsActions(t *testing.T) {
	events := []workerOutputEvent{
		{Payload: json.RawMessage(`{"type":"keep_alive"}`)},
		{Payload: json.RawMessage(`{
			"type":"control_request",
			"uuid":"control-uuid",
			"request_id":"request-id",
			"request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"tool-id","input":{}}
		}`)},
		{Payload: json.RawMessage(`{
			"type":"assistant",
			"uuid":"assistant-uuid",
			"message":{"role":"assistant","content":[{"type":"text","text":"ready"}]}
		}`)},
	}

	prepared, err := prepareWorkerOutputEvents("cse_test", events, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatalf("prepareWorkerOutputEvents() error = %v", err)
	}
	if len(prepared) != len(events) {
		t.Fatalf("prepared event count = %d, want %d", len(prepared), len(events))
	}
	if prepared[0].controlRequest != nil || len(prepared[0].publicPayloads) != 0 {
		t.Fatalf("keep-alive action = %#v, want empty", prepared[0])
	}
	if prepared[1].controlRequest == nil || prepared[1].metadata.EventType != "control_request" {
		t.Fatalf("control action = %#v", prepared[1])
	}
	if len(prepared[2].publicPayloads) == 0 {
		t.Fatalf("public action = %#v, want payloads", prepared[2])
	}
}

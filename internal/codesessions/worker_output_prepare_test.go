package codesessions

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPrepareWorkerOutputEventRejectsInvalidControlRequest(t *testing.T) {
	prepared, err := prepareWorkerOutputEvent(
		"cse_test",
		workerOutputEvent{Payload: json.RawMessage(`{"type":"control_request","uuid":"control-uuid","request_id":"request-id","request":42}`)},
		time.Unix(1, 0).UTC(),
	)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("prepareWorkerOutputEvent() error = %v, want ErrProtocol", err)
	}
	if prepared != nil {
		t.Fatalf("prepared action = %#v, want nil", prepared)
	}
}

func TestPrepareWorkerOutputEventBuildsKeepAliveAction(t *testing.T) {
	prepared, err := prepareWorkerOutputEvent(
		"cse_test",
		workerOutputEvent{Payload: json.RawMessage(`{"type":"keep_alive"}`)},
		time.Unix(1, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("prepareWorkerOutputEvent() error = %v", err)
	}
	if _, ok := prepared.(preparedKeepAliveAction); !ok {
		t.Fatalf("prepared action = %T, want preparedKeepAliveAction", prepared)
	}
}

func TestPrepareWorkerOutputEventIgnoresNonEphemeralStream(t *testing.T) {
	prepared, err := prepareWorkerOutputEvent(
		"cse_test",
		workerOutputEvent{Payload: json.RawMessage(`{
			"type":"stream_event",
			"uuid":"stream-uuid",
			"event":{"type":"message_start","message":{"id":"msg_test"}}
		}`)},
		time.Unix(1, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("prepareWorkerOutputEvent() error = %v", err)
	}
	if _, ok := prepared.(preparedNoopAction); !ok {
		t.Fatalf("prepared action = %T, want preparedNoopAction", prepared)
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
		{Ephemeral: true, Payload: json.RawMessage(`{
			"type":"stream_event",
			"uuid":"stream-uuid",
			"event":{"type":"message_start","message":{"id":"msg_test"}}
		}`)},
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
	if _, ok := prepared[0].(preparedKeepAliveAction); !ok {
		t.Fatalf("prepared[0] = %T, want preparedKeepAliveAction", prepared[0])
	}
	stream, ok := prepared[1].(preparedStreamAction)
	if !ok || len(stream.payload) == 0 {
		t.Fatalf("prepared[1] = %#v, want preparedStreamAction", prepared[1])
	}
	var streamPayload workerOutputCommonPayload
	if err := json.Unmarshal(stream.payload, &streamPayload); err != nil {
		t.Fatalf("decode stream payload: %v", err)
	}
	if streamPayload.UUID != "stream-uuid" || streamPayload.SessionID != "cse_test" {
		t.Fatalf("normalized stream identity = %#v, want original uuid and code session id", streamPayload)
	}
	if streamPayload.CreatedAt == "" || streamPayload.Timestamp != streamPayload.CreatedAt {
		t.Fatalf("normalized stream timestamps = %#v, want matching created_at and timestamp", streamPayload)
	}
	control, ok := prepared[2].(preparedControlAction)
	if !ok || control.metadata.EventType != "control_request" {
		t.Fatalf("prepared[2] = %#v, want preparedControlAction", prepared[2])
	}
	public, ok := prepared[3].(preparedPublicAction)
	if !ok || len(public.payloads) == 0 {
		t.Fatalf("prepared[3] = %#v, want preparedPublicAction", prepared[3])
	}
}

func TestLeadingWorkerStreamPayloadsStopsAtNonStreamEvent(t *testing.T) {
	actions := []preparedWorkerOutputEvent{
		preparedStreamAction{payload: json.RawMessage(`{"sequence":1}`)},
		preparedStreamAction{payload: json.RawMessage(`{"sequence":2}`)},
		preparedPublicAction{payloads: []json.RawMessage{json.RawMessage(`{"type":"agent.message"}`)}},
		preparedStreamAction{payload: json.RawMessage(`{"sequence":3}`)},
	}

	payloads := leadingWorkerStreamPayloads(actions)
	if len(payloads) != 2 || string(payloads[0]) != `{"sequence":1}` || string(payloads[1]) != `{"sequence":2}` {
		t.Fatalf("leadingWorkerStreamPayloads() = %q, want first two stream payloads", payloads)
	}
}

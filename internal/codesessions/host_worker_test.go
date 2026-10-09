package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestHostModeUsesPersistedConfig(t *testing.T) {
	for _, test := range []struct {
		name, metadata string
		want           bool
	}{
		{name: "invalid", metadata: `{`},
		{name: "untrusted top level", metadata: `{"agent_runtime_mode":"host"}`},
		{name: "legacy", metadata: `{"config":{"agent_runtime_mode":"sandbox"}}`},
		{name: "host", metadata: `{"config":{"agent_runtime_mode":"host"}}`, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isHostCodeSession(db.CodeSession{Metadata: json.RawMessage(test.metadata)}); got != test.want {
				t.Fatalf("host mode = %v", got)
			}
		})
	}
}

func TestHostHistoryRejectsInvalidAndPreservesProviderData(t *testing.T) {
	for _, entry := range []HostHistoryEntry{
		{Role: "message", RunID: "run", Payload: json.RawMessage(`{}`)},
		{ID: "id", Role: "message", Payload: json.RawMessage(`{}`)},
		{ID: "id", Role: "unknown", RunID: "run", Payload: json.RawMessage(`{}`)},
		{ID: "id", Role: "message", RunID: "run", Payload: json.RawMessage(`{`)},
	} {
		if _, err := hostHistoryInput("cse_test", entry); !errors.Is(err, ErrHostHistoryInvalid) {
			t.Fatalf("invalid entry accepted: %#v, %v", entry, err)
		}
	}
	entry := HostHistoryEntry{ID: "input_a", Role: "message", RunID: HostRunID("cse_test", "input_a"), InputEventID: "input_a", Payload: json.RawMessage(`{"role":"assistant","content":[{"type":"reasoning","signature":"opaque-signature"}]}`)}
	first, err := hostHistoryInput("cse_test", entry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hostHistoryInput("cse_test", entry)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExternalID != second.ExternalID || first.IdempotencyKey != second.IdempotencyKey {
		t.Fatal("replay changed history identity")
	}
	var envelope hostHistoryEnvelope
	if err := json.Unmarshal(first.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope.Entry.Payload) != string(entry.Payload) || envelope.Entry.InputEventID != "input_a" || envelope.SchemaVersion != 1 {
		t.Fatalf("history lost provider data: %s", first.Payload)
	}
	if HostRunID("cse_other", "input_a") == entry.RunID {
		t.Fatal("run identity crossed session scope")
	}
}

func TestHostPermissionPayloadValidation(t *testing.T) {
	for _, input := range []HostToolRequest{
		{},
		{RequestID: "request", ToolUseID: "tool", ToolName: "Bash", Input: json.RawMessage(`null`)},
		{RequestID: "request", ToolUseID: "tool", ToolName: "Bash", Input: json.RawMessage(`[]`)},
	} {
		if _, err := hostPermissionPayload(input); !errors.Is(err, ErrHostPermissionInvalid) {
			t.Fatalf("invalid permission accepted: %v", err)
		}
	}
	payload, err := hostPermissionPayload(HostToolRequest{RequestID: "request", ToolUseID: "tool", ToolName: "Bash", Input: json.RawMessage(`{"command":"pwd"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Request.Subtype != "can_use_tool" || payload.Request.ToolUseID != "tool" || payload.Request.Input["command"] != "pwd" || payload.SessionThreadID != "" {
		t.Fatalf("permission payload = %#v", payload)
	}
}

func TestHostPublicPayloadKeepsStablePreviewAndFinalIDs(t *testing.T) {
	if _, err := hostPublicPayload(json.RawMessage(`{"type":"agent.message"}`)); !errors.Is(err, ErrProtocol) {
		t.Fatal("missing stable ID accepted")
	}
	for _, raw := range []string{
		`{"type":"event_start","event":{"type":"agent.message","id":"message_id"}}`,
		`{"type":"event_delta","event_id":"message_id","delta":{"text":"hi"}}`,
		`{"type":"agent.message","id":"message_id","content":[{"type":"text","text":"hi"}]}`,
	} {
		encoded, err := hostPublicPayload(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		header, err := decodeWorkerPayloadHeader(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if header.UUID != "message_id" {
			t.Fatalf("identity = %s", encoded)
		}
		if _, err := prepareWorkerOutputEvent("cse_test", workerOutputEvent{Payload: encoded}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHostFailedTurnPreservesTerminalReasonThroughWorkerMapping(t *testing.T) {
	at := time.Date(2026, 10, 9, 10, 11, 12, 0, time.UTC)
	payload, err := hostFailedTurnPayload(at)
	if err != nil {
		t.Fatal(err)
	}
	payload, err = hostPublicPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	action, err := prepareWorkerOutputEvent("cse_test", workerOutputEvent{Payload: payload}, at)
	if err != nil {
		t.Fatal(err)
	}
	public, ok := action.(preparedPublicAction)
	if !ok || len(public.payloads) != 1 {
		t.Fatalf("failure did not become public status: %#v", action)
	}
	var event struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		ProcessedAt string `json:"processed_at"`
		StopReason  struct {
			Type string `json:"type"`
		} `json:"stop_reason"`
	}
	if err := json.Unmarshal(public.payloads[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.ID == "" || event.Type != "session.status_idle" || event.StopReason.Type != "retries_exhausted" || event.ProcessedAt != formatTime(at) {
		t.Fatalf("terminal status contract changed: %s", public.payloads[0])
	}
}

type hostModelSink struct {
	events  []json.RawMessage
	failure error
}

type hostPreviewSink struct {
	publication workerStreamPublication
	failure     error
}

func (s *hostPreviewSink) PublishCodeSessionEvents(context.Context, db.CodeSession, []json.RawMessage) error {
	return errors.New("preview reached persistent output")
}

func (s *hostPreviewSink) PublishCodeSessionStreamEvent(_ context.Context, route CodeSessionStreamRoute, epoch int64, payload json.RawMessage) error {
	s.publication = workerStreamPublication{route: route, workerEpoch: epoch, payload: payload}
	return s.failure
}

func TestHostPreviewPublishesNormalizedPayloadAndPropagatesFailure(t *testing.T) {
	failure := errors.New("fanout unavailable")
	sink := &hostPreviewSink{failure: failure}
	worker := &HostWorker{service: &Service{sink: sink}, record: db.CodeSession{ExternalID: "cse_test", WorkspaceUUID: "workspace", SessionExternalID: "session"}, epoch: 7}
	if err := worker.publishPreview(context.Background(), json.RawMessage(`{"type":"event_start","event":{"type":"agent.message","id":"message"}}`)); !errors.Is(err, failure) {
		t.Fatalf("fanout failure swallowed: %v", err)
	}
	worker.service.sink = nil
	if err := worker.publishPreview(context.Background(), json.RawMessage(`{}`)); !errors.Is(err, db.ErrInvalidState) {
		t.Fatalf("missing sink accepted: %v", err)
	}
	worker.service.sink = sink
	sink.failure = nil
	for _, raw := range []string{
		`{"type":"event_start","event":{"type":"agent.message","id":"message"}}`,
		`{"type":"event_delta","event_id":"message","delta":{"type":"content_delta","index":0,"content":{"type":"text","text":"hi"}}}`,
	} {
		payload, err := hostPublicPayload(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := worker.publishPreview(context.Background(), payload); err != nil {
			t.Fatal(err)
		}
		publication := sink.publication
		if publication.workerEpoch != 7 || publication.route.CodeSessionID != "cse_test" || publication.route.WorkspaceUUID != "workspace" || publication.route.SessionExternalID != "session" || string(publication.payload) != string(payload) {
			t.Fatalf("preview routing changed: %#v", publication)
		}
	}
}

func (s *hostModelSink) PublishCodeSessionEvents(_ context.Context, _ db.CodeSession, events []json.RawMessage) error {
	s.events = append(s.events, events...)
	return s.failure
}

func (s *hostModelSink) PublishCodeSessionStreamEvent(context.Context, CodeSessionStreamRoute, int64, json.RawMessage) error {
	return nil
}

func TestHostProxyRetainsUsageWithoutDuplicateOutput(t *testing.T) {
	failure := errors.New("write failed")
	sink := &hostModelSink{failure: failure}
	service := &Service{sink: sink}
	request := &ModelRequest{StartID: "request", CodeSessionID: "cse_test", ThreadID: "thread", Model: "model", codeSession: db.CodeSession{Metadata: json.RawMessage(`{"config":{"agent_runtime_mode":"host"}}`)}}
	result := ModelRequestResult{EndedAt: time.Now(), Messages: []ModelRequestMessage{{ID: "message", Type: "agent.message"}}, ToolUses: []ModelRequestToolUse{{ID: "tool", Name: "Bash"}}, ToolUseIDs: []string{"tool"}, EventIDs: []string{"message"}, Usage: ModelRequestUsage{OutputTokens: new(int64(3))}}
	if err := service.EndModelRequest(context.Background(), request, result); !errors.Is(err, failure) {
		t.Fatalf("write failure swallowed: %v", err)
	}
	sink.failure = nil
	sink.events = nil
	if err := service.EndModelRequest(context.Background(), request, result); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("proxy emitted %d events", len(sink.events))
	}
	var event modelRequestEvent
	if err := json.Unmarshal(sink.events[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "span.model_request_end" || len(event.ToolUseIDs) != 0 || len(event.EventIDs) != 0 || event.Usage == nil || event.Usage.OutputTokens == nil || *event.Usage.OutputTokens != 3 {
		t.Fatalf("gateway event = %s", sink.events[0])
	}
}

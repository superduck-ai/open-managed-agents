package sessions

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestMessageStreamPreviewRejectsInvalidEvents(t *testing.T) {
	for _, raw := range []string{`{"type":"event_delta","event_id":"a","delta":{"type":"content_delta","index":0,"content":{"type":"thinking","thinking":"private"}}}`, `{}`, `{"type":"event_start","event":{"type":"agent.tool_use","id":"a"}}`, `{"type":"event_delta","event_id":"a","delta":{"type":"content_delta","index":1,"content":{"type":"text"}}}`} {
		if _, ok := messageStreamPreview(codeSessionStreamFanout{WorkspaceUUID: "workspace", SessionExternalID: "session", WorkerEpoch: 2, Payload: json.RawMessage(raw)}); ok {
			t.Fatalf("accepted invalid preview %s", raw)
		}
	}
}

func TestMessageStreamPreviewPreservesIdentityAndThread(t *testing.T) {
	for _, kind := range []string{"agent.message", "agent.thinking"} {
		raw, err := json.Marshal(map[string]any{"type": "event_start", "event": map[string]string{"id": "event-a", "type": kind}, "session_thread_id": "thread-a"})
		if err != nil {
			t.Fatal(err)
		}
		event, ok := messageStreamPreview(codeSessionStreamFanout{WorkspaceUUID: "workspace", SessionExternalID: "session", WorkerEpoch: 2, Payload: raw})
		if !ok || event.ExternalID != "event-a" || event.WorkspaceUUID != "workspace" || event.SessionExternalID != "session" || event.PrimaryThread || event.ThreadExternalID == nil || *event.ThreadExternalID != "thread-a" {
			t.Fatalf("invalid routed preview: %#v", event)
		}
	}
}

func TestHostFailureRetiresMessagePreviews(t *testing.T) {
	ids := previewsFinishedBy(sessionStreamEvent{EventType: "system.message", Payload: json.RawMessage(`{"event_ids":["preview-a","preview-b"]}`)})
	if !slices.Equal(ids, []string{"preview-a", "preview-b"}) {
		t.Fatalf("retirement IDs = %v", ids)
	}
}

func TestHarnessStreamRequiresLiveRequestAndContiguousDeltas(t *testing.T) {
	batch := codeSessionStreamFanout{WorkspaceUUID: "workspace", SessionExternalID: "session", WorkerEpoch: 2, ModelRequestID: "request"}
	preview := func(sequence uint64, raw string) sessionEventDelivery {
		batch.PreviewSequence, batch.Payload = sequence, json.RawMessage(raw)
		event, ok := messageStreamPreview(batch)
		if !ok {
			t.Fatal("invalid fixture preview")
		}
		return sessionEventDelivery{event: event}
	}
	start := preview(1, `{"type":"event_start","event":{"type":"agent.message","id":"text"}}`)
	delta := func(sequence uint64) sessionEventDelivery {
		return preview(sequence, `{"type":"event_delta","event_id":"text","delta":{"type":"content_delta","index":1,"content":{"type":"text","text":"片段"}}}`)
	}
	request := sessionStreamEvent{ExternalID: "request", PrimaryThread: true, EventType: "span.model_request_start"}
	end := sessionEventDelivery{event: sessionStreamEvent{ExternalID: "request_end", PrimaryThread: true, EventType: "span.model_request_end", Payload: json.RawMessage(`{"event_ids":["text"]}`)}}
	c := newStreamConnection("primary", true, map[string]struct{}{"agent.message": {}})
	c.accepts(request)
	if _, ok := c.event(start); ok {
		t.Fatal("late subscriber should not start a preview after replaying the request")
	}
	c.event(sessionEventDelivery{event: request})
	for i, test := range []struct {
		delivery sessionEventDelivery
		want     bool
	}{
		{start, true}, {start, false}, {delta(2), true}, {delta(4), false}, {delta(5), false},
		{end, true}, {delta(3), false}, {start, false},
	} {
		if _, ok := c.event(test.delivery); ok != test.want {
			t.Fatalf("event %d accepted=%t, want %t", i, ok, test.want)
		}
	}
}

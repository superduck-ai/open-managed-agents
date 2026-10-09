package sessions

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestNativeStreamPreviewRejectsInvalidEvents(t *testing.T) {
	for _, raw := range []string{`{}`, `{"type":"event_start","event":{"type":"agent.tool_use","id":"a"}}`, `{"type":"event_delta","event_id":"a","delta":{"type":"content_delta","index":1,"content":{"type":"text"}}}`} {
		if _, ok := nativeStreamPreview(codeSessionStreamFanout{WorkspaceUUID: "workspace", SessionExternalID: "session", WorkerEpoch: 2, Payload: json.RawMessage(raw)}); ok {
			t.Fatalf("accepted invalid preview %s", raw)
		}
	}
}

func TestNativeStreamPreviewPreservesIdentityAndThread(t *testing.T) {
	for _, kind := range []string{"agent.message", "agent.thinking"} {
		raw, err := json.Marshal(map[string]any{"type": "event_start", "event": map[string]string{"id": "event-a", "type": kind}, "session_thread_id": "thread-a"})
		if err != nil {
			t.Fatal(err)
		}
		event, ok := nativeStreamPreview(codeSessionStreamFanout{WorkspaceUUID: "workspace", SessionExternalID: "session", WorkerEpoch: 2, Payload: raw})
		if !ok || event.ExternalID != "event-a" || event.WorkspaceUUID != "workspace" || event.SessionExternalID != "session" || event.PrimaryThread || event.ThreadExternalID == nil || *event.ThreadExternalID != "thread-a" {
			t.Fatalf("invalid routed preview: %#v", event)
		}
	}
}

func TestHostFailureRetiresNativePreviews(t *testing.T) {
	ids := previewsFinishedBy(sessionStreamEvent{EventType: "system.message", Payload: json.RawMessage(`{"event_ids":["preview-a","preview-b"]}`)})
	if !slices.Equal(ids, []string{"preview-a", "preview-b"}) {
		t.Fatalf("retirement IDs = %v", ids)
	}
}

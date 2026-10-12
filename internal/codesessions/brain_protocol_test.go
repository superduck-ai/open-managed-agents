package codesessions

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBrainPreviewUsesCCREphemeralStreamPath(t *testing.T) {
	event, err := prepareWorkerOutputEvent("cse_test", workerOutputEvent{
		Ephemeral: true, Payload: json.RawMessage(`{"type":"event_delta","uuid":"delta","event_id":"preview","delta":{"type":"content_delta","content":{"type":"text","text":"hello"}}}`),
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := event.(preparedStreamAction)
	if !ok {
		t.Fatalf("preview persisted as %T", event)
	}
	var payload workerOutputCommonPayload
	if err := json.Unmarshal(stream.payload, &payload); err != nil || payload.Type != "event_delta" || payload.UUID != "delta" {
		t.Fatalf("invalid preview: %+v %v", payload, err)
	}
}

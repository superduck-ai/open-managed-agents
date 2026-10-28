package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWorkerPayloadCarriesOnlyServerStoredTraceContext(t *testing.T) {
	forged := db.SessionEvent{ExternalID: "input", EventType: "user.message", CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`{"id":"input","type":"user.message","content":[],"trace_context":{"traceparent":"forged"}}`)}
	payload, err := workerPayloadForPublicEvent("code", forged)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		TraceContext map[string]string `json:"trace_context"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil || wire.TraceContext != nil {
		t.Fatalf("payload-supplied trace context survived: %s", payload)
	}
	parent, state := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "vendor=a"
	stored := forged
	stored.TraceParent = &parent
	payload, err = workerPayloadForPublicEvent("code", stored)
	if err != nil || json.Unmarshal(payload, &wire) != nil || wire.TraceContext["traceparent"] != parent || wire.TraceContext["tracestate"] != "" {
		t.Fatalf("trace context = %s %v", payload, err)
	}
	stored.TraceState = &state
	payload, _ = workerPayloadForPublicEvent("code", stored)
	if json.Unmarshal(payload, &wire) != nil || wire.TraceContext["tracestate"] != state {
		t.Fatalf("trace state = %s", payload)
	}
}

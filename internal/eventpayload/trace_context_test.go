package eventpayload

import (
	"bytes"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestTraceContextStaysInternal(t *testing.T) {
	store := New(nil, nil)
	parent, state := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "vendor=a"
	broken := db.SessionEvent{EventType: "user.message", Payload: []byte(`{"type":"user.message","_trace_context":"oops"}`)}
	if _, err := store.RestorePublic(t.Context(), broken); err == nil {
		t.Fatal("malformed stored trace context was accepted")
	}
	prepared, err := store.PreparePublic(t.Context(), "org", "workspace", []db.SessionEvent{
		{ExternalID: "interrupt", EventType: "user.interrupt", Payload: []byte(`{"type":"user.interrupt"}`), TraceParent: &parent},
		{ExternalID: "plain", EventType: "user.message", Payload: []byte(`{"type":"user.message","content":[]}`)},
		{ExternalID: "traced", EventType: "user.message", Payload: []byte(`{"type":"user.message","content":[]}`), TraceParent: &parent, TraceState: &state},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(prepared[0].Payload, []byte(TraceContextKey)) || bytes.Contains(prepared[1].Payload, []byte(TraceContextKey)) {
		t.Fatalf("trace context stored on untraced events: %s %s", prepared[0].Payload, prepared[1].Payload)
	}
	if !bytes.Contains(prepared[2].Payload, []byte(`"`+TraceContextKey+`":{"traceparent":"`+parent+`","tracestate":"vendor=a"}`)) {
		t.Fatalf("stored payload = %s", prepared[2].Payload)
	}
	persisted := prepared[2]
	persisted.TraceParent, persisted.TraceState = nil, nil
	restored, err := store.RestorePublic(t.Context(), persisted)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(restored.Payload, []byte(TraceContextKey)) || restored.TraceParent == nil || *restored.TraceParent != parent || *restored.TraceState != state {
		t.Fatalf("restored = %s %+v", restored.Payload, restored)
	}
	created, err := RestoreCreatedPublic([]db.SessionEvent{persisted}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(created[0].Payload, []byte(TraceContextKey)) || created[0].TraceParent == nil || *created[0].TraceParent != parent {
		t.Fatalf("created = %s %+v", created[0].Payload, created[0])
	}
	blobUUID := "blob"
	summary := db.SessionEvent{ExternalID: "large", UUID: "u", EventType: "user.message", PayloadBlobUUID: &blobUUID, Payload: []byte(`{"type":"user.message","blob_ref":{"version":1,"id":"epb_blob"},"_trace_context":{"traceparent":"` + parent + `"}}`)}
	original := db.SessionEvent{ExternalID: "large", UUID: "u", EventType: "user.message", Payload: []byte(`{"type":"user.message","content":[]}`)}
	created, err = RestoreCreatedPublic([]db.SessionEvent{summary}, []db.SessionEvent{original})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(created[0].Payload, original.Payload) || created[0].TraceParent == nil || *created[0].TraceParent != parent || created[0].TraceState != nil {
		t.Fatalf("externalized created = %s %+v", created[0].Payload, created[0])
	}
}

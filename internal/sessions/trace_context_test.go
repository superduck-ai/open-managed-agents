package sessions

import (
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestAttachTraceContextOnlyTagsUserMessages(t *testing.T) {
	events := []db.SessionEvent{{EventType: "user.message"}, {EventType: "user.interrupt"}, {EventType: "user.message"}}
	attachTraceContext(httptest.NewRequest("POST", "/", nil), events)
	if events[0].TraceParent != nil {
		t.Fatal("missing header attached a trace context")
	}
	request := httptest.NewRequest("POST", "/", nil)
	request.Header.Set("traceparent", "00-00000000000000000000000000000000-00f067aa0ba902b7-01")
	attachTraceContext(request, events)
	if events[0].TraceParent != nil {
		t.Fatal("invalid traceparent was stored")
	}
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	request.Header.Set("tracestate", "vendor=a")
	attachTraceContext(request, events)
	if events[0].TraceParent == nil || *events[2].TraceState != "vendor=a" || events[1].TraceParent != nil {
		t.Fatalf("events = %+v", events)
	}
}

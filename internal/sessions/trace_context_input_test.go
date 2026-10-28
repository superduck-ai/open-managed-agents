package sessions

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
)

func TestClientCannotSupplyStoredTraceContext(t *testing.T) {
	raw := json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}],"_trace_context":{"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}}`)
	event, _, _, err := normalizeInputEvent(db.Session{}, raw, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(event.Payload, []byte(eventpayload.TraceContextKey)) {
		t.Fatalf("client trace context survived: %s", event.Payload)
	}
}

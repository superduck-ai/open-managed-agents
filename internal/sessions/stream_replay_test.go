package sessions

import "testing"

func TestStreamReplaySuppressesOverlappingLiveEvents(t *testing.T) {
	thread := "thread"
	connection := newStreamConnection(thread, true, nil)
	event := sessionStreamEvent{ExternalID: "replayed", EventType: "span.model_request_start", ThreadExternalID: &thread}
	connection.rememberReplay("cursor", []sessionStreamEvent{event})
	for _, id := range []string{"cursor", "replayed", "replayed"} {
		event.ExternalID = id
		if _, accepted := connection.event(sessionEventDelivery{event: event}); accepted {
			t.Fatalf("replayed event %s delivered again", id)
		}
	}
	event.ExternalID = "live"
	if _, accepted := connection.event(sessionEventDelivery{event: event}); !accepted {
		t.Fatal("new live event rejected")
	}
	connection = newStreamConnection(thread, true, nil)
	connection.rememberReplay("", nil)
	if !connection.accepts(event) {
		t.Fatal("connection without cursor changed")
	}
}

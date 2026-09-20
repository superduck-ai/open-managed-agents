package dreams

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDreamSessionFinishedRequiresIdleAndLatestModelEnd(t *testing.T) {
	start := db.SessionEvent{EventType: "span.model_request_start"}
	end := db.SessionEvent{EventType: "span.model_request_end"}
	idle := db.Session{Status: "idle"}
	running := db.Session{Status: "running"}

	if dreamSessionFinished(idle, []db.SessionEvent{start}) {
		t.Fatal("tool-turn idle after start only must not complete the Dream")
	}
	if dreamSessionFinished(running, []db.SessionEvent{start, end}) {
		t.Fatal("model end while Session still running must not complete the Dream")
	}
	if dreamSessionFinished(idle, []db.SessionEvent{start, end, start}) {
		t.Fatal("idle after a later model start must not complete the Dream")
	}
	if !dreamSessionFinished(idle, []db.SessionEvent{start, end}) {
		t.Fatal("idle after the latest model end should complete the Dream")
	}
	if !dreamSessionFinished(idle, []db.SessionEvent{start, end, start, end}) {
		t.Fatal("idle after a later model end should complete the Dream")
	}
}

func TestAggregateDreamUsagePrefersLatestModelEndOverMessageTurns(t *testing.T) {
	events := []db.SessionEvent{
		{EventType: "agent.message", Payload: json.RawMessage(`{"message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":2}}}`)},
		{EventType: "span.model_request_end", Payload: json.RawMessage(`{"usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":5,"cache_creation_input_tokens":7}}`)},
	}
	got := aggregateDreamUsage(events, json.RawMessage(`{"input_tokens":1}`))
	want := `{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":5,"cache_creation_input_tokens":7}`
	if string(got) != want {
		t.Fatalf("usage = %s, want %s", got, want)
	}
}

func TestAggregateDreamUsageSumsUniqueAgentMessagesWhenEndIsMissing(t *testing.T) {
	events := []db.SessionEvent{
		{EventType: "agent.message", Payload: json.RawMessage(`{"message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":4}}}`)},
		{EventType: "agent.message", Payload: json.RawMessage(`{"id":"split","message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":4}}}`)},
		{EventType: "agent.message", Payload: json.RawMessage(`{"message":{"id":"msg_2","usage":{"input_tokens":3,"output_tokens":1,"cache_creation_input_tokens":8}}}`)},
	}
	got := aggregateDreamUsage(events, json.RawMessage(`{}`))
	want := `{"input_tokens":13,"output_tokens":3,"cache_read_input_tokens":4,"cache_creation_input_tokens":8}`
	if string(got) != want {
		t.Fatalf("usage = %s, want %s", got, want)
	}
}

func TestAggregateDreamUsageFallsBackToSessionColumn(t *testing.T) {
	got := aggregateDreamUsage(nil, json.RawMessage(`{"input_tokens":9,"output_tokens":4}`))
	want := `{"input_tokens":9,"output_tokens":4,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}`
	if string(got) != want {
		t.Fatalf("usage = %s, want %s", got, want)
	}
}

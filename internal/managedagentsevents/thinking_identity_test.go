package managedagentsevents

import (
	"encoding/json"
	"os"
	"testing"
)

func TestThinkingIdentitySharedWithWorker(t *testing.T) {
	raw, err := os.ReadFile("testdata/thinking-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		CodeSessionID string `json:"codeSessionID"`
		MessageID     string `json:"messageID"`
		Index         int    `json:"index"`
		EventID       string `json:"eventID"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		if got := StableAssistantEventID(v.CodeSessionID, v.MessageID, v.Index, "agent.thinking"); got != v.EventID {
			t.Fatalf("thinking ID=%s, want %s", got, v.EventID)
		}
	}
}

func TestThinkingDeltaFilter(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		thinking bool
	}{
		{`{"type":"event_delta","delta":{"type":"content_delta","content":{"type":"text","text":"answer"}}}`, false},
		{`{"type":"event_start","event":{"type":"agent.thinking","id":"thinking"}}`, false},
		{`{"type":"event_delta","delta":{"type":"content_delta","content":{"type":"thinking","thinking":"private"}}}`, true},
	} {
		if IsThinkingDelta(json.RawMessage(tc.raw)) != tc.thinking {
			t.Fatalf("unexpected delta classification: %s", tc.raw)
		}
	}
}

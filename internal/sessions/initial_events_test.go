package sessions

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestNormalizeSessionInitialEventsRejectsInvalidBatch(t *testing.T) {
	message := `{"type":"user.message","content":[{"type":"text","text":"first"}]}`
	outcome := `{"type":"user.define_outcome","description":"done","rubric":{"type":"text","content":"done"}}`
	cases := []string{`null`, `{}`, `[null]`, `[1]`, `[{}]`, `[{"type":"system.message","content":[]}]`, `[{"type":"user.interrupt"}]`, `[{"type":"user.tool_confirmation"}]`, `[{"type":"user.tool_result"}]`, `[{"type":"user.custom_tool_result"}]`, `[{"type":"user.message","content":[]}]`, `[{"type":"user.message","session_thread_id":"sthr_other","content":[{"type":"text","text":"x"}]}]`, `[` + outcome + `,` + outcome + `]`, `[{"type":"user.define_outcome","description":"done"}]`, `[` + strings.Repeat(message+`,`, 50) + message + `]`, `[` + message + `,{"type":"user.interrupt"}]`}
	for _, raw := range cases {
		t.Run(raw[:min(len(raw), 65)], func(t *testing.T) {
			s := db.Session{OutcomeEvaluations: json.RawMessage(`[]`)}
			events, err := normalizeSessionInitialEvents(&s, json.RawMessage(raw), time.Now())
			if err == nil || events != nil {
				t.Fatalf("invalid batch accepted: %s", raw)
			}
		})
	}
}

func TestNormalizeSessionInitialEventsEmptyAndOrdered(t *testing.T) {
	for _, raw := range []string{"", `[]`} {
		s := db.Session{}
		events, e := normalizeSessionInitialEvents(&s, json.RawMessage(raw), time.Now())
		if e != nil || len(events) != 0 {
			t.Fatalf("empty: %v %v", events, e)
		}
	}
	raw := `[{"type":"user.define_outcome","description":"done","rubric":{"type":"text","content":"done"}},{"type":"user.message","content":[{"type":"text","text":"A"}]},{"type":"user.message","content":[{"type":"text","text":"B"}]}]`
	s := db.Session{OutcomeEvaluations: json.RawMessage(`[]`)}
	events, e := normalizeSessionInitialEvents(&s, json.RawMessage(raw), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if len(events) != 4 || events[0].EventType != "session.status_running" || events[1].EventType != "user.define_outcome" || !strings.Contains(string(events[2].Payload), `"A"`) || !strings.Contains(string(events[3].Payload), `"B"`) {
		t.Fatalf("order: %+v", events)
	}
	seen := map[string]bool{}
	for _, event := range events {
		if event.UUID == "" || !strings.HasPrefix(event.ExternalID, "sevt_") || seen[event.ExternalID] {
			t.Fatal("missing or duplicate identity")
		}
		seen[event.ExternalID] = true
	}
	var evaluations []any
	if json.Unmarshal(s.OutcomeEvaluations, &evaluations) != nil || len(evaluations) != 1 {
		t.Fatal("outcome not retained")
	}
	if !events[2].ProcessedAt.IsZero() {
		t.Fatal("unconsumed message marked processed")
	}
}

func TestNormalizeSessionInitialEventsDocumentLimit(t *testing.T) {
	block := `{"type":"document","source":{"type":"file","file_id":"file_test"}}`
	event := func(n int) string {
		return `{"type":"user.message","content":[` + strings.TrimSuffix(strings.Repeat(block+`,`, n), ",") + `]}`
	}
	for _, n := range []int{50, 51} {
		s := db.Session{}
		raw := `[` + event(50) + `,` + event(n) + `]`
		_, e := normalizeSessionInitialEvents(&s, json.RawMessage(raw), time.Now())
		if (e != nil) != (n == 51) {
			t.Fatalf("documents=%d error=%v", 50+n, e)
		}
	}
}

func TestNormalizeSessionInitialEventsAcceptsFifty(t *testing.T) {
	raw := `[` + strings.TrimSuffix(strings.Repeat(`{"type":"user.message","content":[{"type":"text","text":"x"}]},`, 50), ",") + `]`
	session := db.Session{}
	events, err := normalizeSessionInitialEvents(&session, json.RawMessage(raw), time.Now())
	if err != nil || len(events) != 51 {
		t.Fatalf("50 inputs plus running status: count=%d err=%v", len(events), err)
	}
}

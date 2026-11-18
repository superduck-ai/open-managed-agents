package eventpayload

import (
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPreparePublicTitleInput(t *testing.T) {
	store := New(nil, nil)
	for _, tc := range []struct{ kind, payload, want string }{
		{"agent.message", `{"content":[{"type":"text","text":"ignored"}]}`, ""},
		{"user.message", `{"content":[{"type":"image","text":"not user text"}]}`, ""},
		{"user.message", `{"content":[{"type":"text","text":"  \n\u202e "}]}`, ""},
		{"user.message", `{"content":[{"type":"text","text":" fix "},{"type":"image"},{"type":"text","text":"session naming"}]}`, "fix session naming"},
	} {
		events, err := store.PreparePublic(t.Context(), "org", "workspace", []db.SessionEvent{{EventType: tc.kind, Payload: []byte(tc.payload)}})
		if err != nil {
			t.Fatal(err)
		}
		if events[0].TitleInput != tc.want {
			t.Fatalf("%s: %q want %q", tc.payload, events[0].TitleInput, tc.want)
		}
		if !strings.Contains(string(events[0].Payload), "content") {
			t.Fatal("public event changed")
		}
	}
}

func TestPreparePublicTitleInputSkipsLongInvisibleBlock(t *testing.T) {
	payload := `{"content":[{"type":"text","text":"` + strings.Repeat("​", 1400) +
		`"},{"type":"text","text":"fix session naming"}]}`
	events, err := New(nil, nil).PreparePublic(t.Context(), "org", "workspace", []db.SessionEvent{
		{EventType: "user.message", Payload: []byte(payload)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := events[0].TitleInput; got != "fix session naming" {
		t.Fatalf("title input = %q, want visible text from the same message", got)
	}
}

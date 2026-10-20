package sessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestClientConfirmationIsNotAnOrphanToolProjection(t *testing.T) {
	for _, tc := range []struct {
		kind string
		hide bool
	}{
		{"user.tool_confirmation", false},
		{"agent.tool_result", true},
		{"agent.tool_use", true},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"id": "event", "type": tc.kind, "tool_use_id": "child-use", "result": "allow"})
			if err != nil {
				t.Fatal(err)
			}
			event := db.SessionEvent{EventType: tc.kind, Payload: payload}
			if got := primaryToolProjectionHidden(event, map[string]struct{}{"child-use": {}}); got != tc.hide {
				t.Fatalf("hidden=%v, want %v", got, tc.hide)
			}
		})
	}
}

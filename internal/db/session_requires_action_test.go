package db

import "testing"

func TestRequiresActionNeedsPendingToolAfterConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		reason       *sessionStopReason
		thread       bool
		pending, all []string
		wantDrop     bool
	}{
		{name: "late worker pause after confirmation", reason: &sessionStopReason{Type: "requires_action", EventIDs: []string{"settled"}}, wantDrop: true},
		{name: "other thread remains paused", reason: &sessionStopReason{Type: "requires_action", EventIDs: []string{"settled"}}, thread: true, all: []string{"other"}, wantDrop: true},
		{name: "live thread pause", reason: &sessionStopReason{Type: "requires_action"}, thread: true, pending: []string{"tool"}, all: []string{"tool"}},
		{name: "live session pause", reason: &sessionStopReason{Type: "requires_action"}, all: []string{"tool"}},
		{name: "explicit stale pause", reason: &sessionStopReason{Type: "requires_action", EventIDs: []string{"tool"}}, wantDrop: true},
		{name: "natural completion", reason: &sessionStopReason{Type: "end_turn"}},
		{name: "no stop reason"},
		{name: "self-hosted failure without pending tools", reason: &sessionStopReason{Type: "requires_action"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := unbackedRequiresAction(tc.reason, "requires_action", tc.thread, tc.pending, tc.all); got != tc.wantDrop {
				t.Fatalf("drop=%t want=%t: worker pause without a pending tool must not become end_turn", got, tc.wantDrop)
			}
		})
	}
}

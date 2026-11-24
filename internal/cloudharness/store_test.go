package cloudharness

import (
	"errors"
	"testing"
)

func TestMemoryStoreRejectsStaleCommit(t *testing.T) {
	ctx := t.Context()
	store := NewMemoryStore()
	first := UserMessage{Text: "first"}
	_, err := store.Commit(ctx, "thread", 0, Transition{Pending: ModelWork{}, Messages: []Message{first}}, "first")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(ctx, "thread", 0, Transition{Messages: []Message{UserMessage{Text: "stale"}}}, "stale")
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("got %v, want ErrVersionConflict", err)
	}
	state, err := store.LoadState(ctx, "thread")
	if err != nil {
		t.Fatal(err)
	}
	history, err := store.LoadHistoryUpTo(ctx, "thread", nil, &state.MessageCount)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 1 || state.Pending != (ModelWork{}) || len(history) != 1 || history[0] != first {
		t.Fatalf("stale commit changed state %#v or history %#v", state, history)
	}
	if found, err := store.HasAppliedInput(ctx, "thread", "stale"); err != nil || found {
		t.Fatalf("stale input recorded: %t, %v", found, err)
	}
	if found, err := store.HasAppliedInput(ctx, "thread", "first"); err != nil || !found {
		t.Fatalf("first input missing: %t, %v", found, err)
	}
}

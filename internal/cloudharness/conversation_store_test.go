package cloudharness

import (
	"errors"
	"slices"
	"testing"
)

func TestMemoryConversationHistoryBounds(t *testing.T) {
	ctx := t.Context()
	var store ConversationStore = NewMemoryStore()
	if _, err := store.LoadHistoryUpTo(ctx, "root", new(int64(-1)), nil); !errors.Is(err, ErrInvalidHistoryOrder) {
		t.Fatalf("negative start got %v, want ErrInvalidHistoryOrder", err)
	}
	appendHistory(t, store, "root", "first", "second", "third")
	history, err := store.LoadHistoryUpTo(ctx, "root", new(int64(1)), new(int64(2)))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(history, []Message{UserMessage{Text: "second"}}) {
		t.Fatalf("unexpected history %#v", history)
	}
}

func TestMemoryConversationHistoryWithAncestors(t *testing.T) {
	ctx := t.Context()
	var store ConversationStore = NewMemoryStore()
	if err := store.EnsureRootThread(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	appendHistory(t, store, "root", "old", "included", "after-spawn")
	for _, summary := range []CompactionSummary{{Summary: "visible", UpToOrder: 1}, {Summary: "past spawn", UpToOrder: 3}} {
		if err := store.SaveCompactionSummary(ctx, "root", summary.Summary, summary.UpToOrder); err != nil {
			t.Fatal(err)
		}
	}
	child, err := store.SpawnThread(ctx, "root", "spawn-child", true, SpawnContext{InheritUpTo: new(int64(2))})
	if err != nil {
		t.Fatal(err)
	}
	appendHistory(t, store, child, "child")
	history, compacted, prefix, err := store.LoadHistoryWithAncestors(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	want := []Message{summaryMessage("visible"), UserMessage{Text: "included"}, UserMessage{Text: "child"}}
	if compacted != nil || prefix != 2 || !slices.Equal(history, want) {
		t.Fatalf("history=%#v compacted=%v prefix=%d, want %#v with prefix 2", history, compacted, prefix, want)
	}
}

func TestMemoryConversationFreshBoundary(t *testing.T) {
	ctx := t.Context()
	var store ConversationStore = NewMemoryStore()
	appendHistory(t, store, "root", "hidden")
	fresh, err := store.SpawnThread(ctx, "root", "spawn-fresh", false, SpawnContext{})
	if err != nil {
		t.Fatal(err)
	}
	appendHistory(t, store, fresh, "fresh")
	child, err := store.SpawnThread(ctx, fresh, "spawn-child", false, SpawnContext{InheritUpTo: new(int64(1))})
	if err != nil {
		t.Fatal(err)
	}
	history, compacted, prefix, err := store.LoadHistoryWithAncestors(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if compacted != nil || prefix != 1 || !slices.Equal(history, []Message{UserMessage{Text: "fresh"}}) {
		t.Fatalf("history=%#v compacted=%v prefix=%d, want fresh parent history", history, compacted, prefix)
	}
}

func appendHistory(t *testing.T, store ConversationStore, threadID ThreadID, texts ...string) {
	t.Helper()
	messages := make([]MessageWithID, 0, len(texts))
	for _, text := range texts {
		messages = append(messages, MessageWithID{Message: UserMessage{Text: text}, ID: text})
	}
	if err := store.AppendMessages(t.Context(), threadID, messages); err != nil {
		t.Fatal(err)
	}
}

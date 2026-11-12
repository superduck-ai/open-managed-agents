//go:build integration

package db

import (
	"sync"
	"testing"
	"time"

	"uuid"
)

func TestSessionEventSeqAllocationPostgres(t *testing.T) {
	ctx, store, scope, _ := newDSHConfirmationTestStore(t, "resolved")
	code, found, err := store.GetCodeSession(ctx, scope.CodeSessionID)
	if err != nil || !found {
		t.Fatal(err)
	}
	primary, found, err := store.GetPrimarySessionThread(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil || !found {
		t.Fatal(err)
	}
	processedEvent := func(id string) SessionEvent {
		now := time.Now().UTC().Truncate(time.Microsecond)
		return SessionEvent{UUID: uuid.NewV4().String(), ExternalID: id, EventType: "agent.thinking", ThreadExternalID: &primary.ExternalID, Payload: []byte(`{"id":"` + id + `","type":"agent.thinking"}`), CreatedAt: now, ProcessedAt: now}
	}
	position := func() int64 {
		seq, err := store.SessionLastEventSeq(ctx, scope.WorkspaceUUID, scope.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		return seq
	}

	t.Run("duplicate insert does not consume a position", func(t *testing.T) {
		event := processedEvent("seq-duplicate")
		if _, err := store.AppendSessionEventsIfAbsent(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event}); err != nil {
			t.Fatal(err)
		}
		before := position()
		changes, err := store.AppendSessionEventsIfAbsent(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event})
		if err != nil || len(changes.Events) != 0 || position() != before {
			t.Fatalf("duplicate inserted=%d before=%d after=%d err=%v", len(changes.Events), before, position(), err)
		}
	})

	t.Run("missing session is rejected without a position", func(t *testing.T) {
		if _, err := store.SessionLastEventSeq(ctx, scope.WorkspaceUUID, "sesn_missing"); err == nil {
			t.Fatal("missing session reported a position")
		}
	})

	t.Run("concurrent producers receive unique consecutive positions", func(t *testing.T) {
		start := position()
		var wait sync.WaitGroup
		results := make(chan SessionEvent, 8)
		for i := range 8 {
			wait.Go(func() {
				changes, err := store.AppendSessionEventsIfAbsent(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{processedEvent("seq-concurrent-" + string(rune('a'+i)))})
				if err != nil || len(changes.Events) == 0 {
					t.Errorf("append changes=%d err=%v", len(changes.Events), err)
					return
				}
				for _, event := range changes.Events {
					results <- event
				}
			})
		}
		wait.Wait()
		close(results)
		seen := map[int64]bool{}
		for event := range results {
			if event.Seq <= start || seen[event.Seq] {
				t.Fatalf("position %d repeated or not after %d", event.Seq, start)
			}
			seen[event.Seq] = true
		}
		if end := position(); end-start != int64(len(seen)) {
			t.Fatalf("counter advanced %d for %d events", end-start, len(seen))
		}
	})

	t.Run("queued input takes the next position only when processed", func(t *testing.T) {
		queued := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "seq-queued", EventType: "user.message", Payload: []byte(`{"id":"seq-queued","type":"user.message","content":[{"type":"text","text":"next"}],"processed_at":null}`), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if _, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{queued}, nil); err != nil {
			t.Fatal(err)
		}
		stored, err := store.GetSessionEvent(ctx, scope.WorkspaceUUID, scope.SessionID, queued.ExternalID)
		if err != nil || stored.Seq != 0 {
			t.Fatalf("queued seq=%d err=%v", stored.Seq, err)
		}
		before := position()
		processed, err := store.ProcessSessionInputsForWorker(ctx, code, scope.Epoch, []string{queued.ExternalID})
		if err != nil || len(processed) != 1 || processed[0].Seq != before+1 || position() != before+1 {
			t.Fatalf("processed=%+v before=%d err=%v", processed, before, err)
		}
		replay, err := store.ProcessSessionInputsForWorker(ctx, code, scope.Epoch, []string{queued.ExternalID})
		if err != nil || len(replay) != 0 || position() != before+1 {
			t.Fatalf("reprocessing consumed a position: %d %v", len(replay), err)
		}
	})
}

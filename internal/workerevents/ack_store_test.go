package workerevents

import (
	"context"
	"testing"
	"time"
)

func TestMemoryAcknowledgementStoreTreatsExpiredReferenceAsMissing(t *testing.T) {
	store := NewMemoryAcknowledgementStore()
	ctx := context.Background()
	if err := store.Put(ctx, "cse_test", 1, "event_test", AckRef{AckSubject: "ack-subject"}); err != nil {
		t.Fatal(err)
	}
	key := acknowledgementKey("cse_test", 1, "event_test")
	entry := store.entries[key]
	entry.expiresAt = time.Now().Add(-time.Second)
	store.entries[key] = entry
	if err := store.Refresh(ctx, "cse_test", 1, "event_test"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(ctx, "cse_test", 1, "event_test"); err != nil || found {
		t.Fatalf("expired ACK reference = (%t, %v), want missing", found, err)
	}
}

func TestMemoryAcknowledgementStoreScopesRefreshAndDelete(t *testing.T) {
	store := NewMemoryAcknowledgementStore()
	ctx := context.Background()
	reference := AckRef{AckSubject: "ack-subject", CleanupJobID: "job_test"}
	if err := store.Put(ctx, "cse_test", 2, "event_test", reference); err != nil {
		t.Fatal(err)
	}
	for _, lookup := range []struct {
		sessionID string
		epoch     int64
		eventID   string
	}{
		{sessionID: "cse_other", epoch: 2, eventID: "event_test"},
		{sessionID: "cse_test", epoch: 3, eventID: "event_test"},
		{sessionID: "cse_test", epoch: 2, eventID: "event_other"},
	} {
		if _, found, err := store.Get(ctx, lookup.sessionID, lookup.epoch, lookup.eventID); err != nil || found {
			t.Fatalf("cross-scope ACK lookup = (%t, %v), want missing", found, err)
		}
	}
	if err := store.Refresh(ctx, "cse_test", 2, "event_test"); err != nil {
		t.Fatal(err)
	}
	if got, found, err := store.Get(ctx, "cse_test", 2, "event_test"); err != nil || !found || got != reference {
		t.Fatalf("refreshed ACK reference = (%+v, %t, %v)", got, found, err)
	}
	if err := store.Delete(ctx, "cse_test", 2, "event_test"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(ctx, "cse_test", 2, "event_test"); err != nil || found {
		t.Fatalf("deleted ACK reference = (%t, %v), want missing", found, err)
	}
}

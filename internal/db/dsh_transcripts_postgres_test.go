//go:build integration

package db

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"github.com/superduck-ai/yourbatis"
)

func TestDSHTranscriptsPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=7,worker_lease_expires_at=now()+interval '1 hour' WHERE external_id=$1`, id)
	session, found, err := store.GetCodeSession(ctx, id)
	if err != nil || !found {
		t.Fatalf("code session=%v,%v", found, err)
	}
	scope := DSHScope{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, CodeSessionUUID: session.UUID, Epoch: 7}
	header := []byte(`{"version":4,"id":"root","createdAt":0,"isSeeded":false}`)
	first := DSHTranscriptEvent{Seq: 0, Payload: []byte(`{"seq":0,"type":"turn/start","data":{"turn":1}}`)}
	t.Run("rejects missing header and wrong owner", func(t *testing.T) {
		if _, err := store.AppendDSHTranscript(ctx, scope, "root", []DSHTranscriptEvent{first}); !errors.Is(err, ErrDSHHeaderRequired) {
			t.Fatalf("missing=%v", err)
		}
		old := scope
		old.Epoch = 6
		if _, err := store.PutDSHTranscript(ctx, old, "root", nil, header); !errors.Is(err, ErrWorkerEpochMismatch) {
			t.Fatalf("old owner=%v", err)
		}
	})
	if _, err := store.PutDSHTranscript(ctx, scope, "root", nil, header); err != nil {
		t.Fatal(err)
	}
	t.Run("gap and conflict roll back all rows", func(t *testing.T) {
		if result, err := store.AppendDSHTranscript(ctx, scope, "root", []DSHTranscriptEvent{{Seq: 2, Payload: []byte(`{"seq":2}`)}}); !errors.Is(err, ErrDSHTranscriptGap) || result.NextSeq != 0 {
			t.Fatalf("gap=%+v,%v", result, err)
		}
		if _, err := store.PutDSHTranscript(ctx, scope, "root", nil, []byte(`{"version":4,"id":"changed"}`)); !errors.Is(err, ErrDSHTranscriptConflict) {
			t.Fatalf("header conflict=%v", err)
		}
	})
	t.Run("batch insert errors leave no rows or watermark changes", func(t *testing.T) {
		if _, err := store.PutDSHTranscript(ctx, scope, "batch", nil, []byte(`{"version":4,"id":"batch","createdAt":0,"isSeeded":false}`)); err != nil {
			t.Fatal(err)
		}
		batch := dshBatchEvents(0, 2)
		batch[1].Payload = []byte(`{"invalid":`)
		if result, err := store.AppendDSHTranscript(ctx, scope, "batch", batch); err == nil || result.NextSeq != 0 {
			t.Fatalf("invalid JSON batch=%+v error=%v", result, err)
		}
		transcript, events, err := store.ReadDSHTranscript(ctx, scope, "batch", 0, 200)
		if err != nil || transcript.NextSeq != 0 || len(events) != 0 {
			t.Fatalf("rollback transcript=%+v events=%d error=%v", transcript, len(events), err)
		}
	})
	t.Run("seq zero and semantic duplicates are immutable", func(t *testing.T) {
		result, err := store.AppendDSHTranscript(ctx, scope, "root", []DSHTranscriptEvent{first})
		if err != nil || result.NextSeq != 1 {
			t.Fatalf("first=%+v,%v", result, err)
		}
		duplicate := DSHTranscriptEvent{Seq: 0, Payload: []byte(`{"data":{"turn":1.0},"type":"turn/start","seq":0}`)}
		result, err = store.AppendDSHTranscript(ctx, scope, "root", []DSHTranscriptEvent{duplicate})
		if err != nil || result.NextSeq != 1 {
			t.Fatalf("duplicate=%+v,%v", result, err)
		}
		if _, err := store.AppendDSHTranscript(ctx, scope, "root", []DSHTranscriptEvent{{Seq: 0, Payload: []byte(`{"seq":0,"type":"different"}`)}}); !errors.Is(err, ErrDSHTranscriptConflict) {
			t.Fatalf("content conflict=%v", err)
		}
		transcript, events, err := store.ReadDSHTranscript(ctx, scope, "root", 0, 200)
		if err != nil || transcript.NextSeq != 1 || len(events) != 1 || events[0].Seq != 0 {
			t.Fatalf("read=%+v,%+v,%v", transcript, events, err)
		}
	})
	t.Run("tenant isolation", func(t *testing.T) {
		other := scope
		other.WorkspaceUUID = "53000000-0000-0000-0000-000000000099"
		if _, _, err := store.ReadDSHTranscript(ctx, other, "root", 0, 200); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross tenant=%v", err)
		}
	})
	t.Run("concurrent identical appends remain one row", func(t *testing.T) {
		next := DSHTranscriptEvent{Seq: 1, Payload: []byte(`{"seq":1,"type":"step/start","data":{"turn":1,"step":1}}`)}
		var wait sync.WaitGroup
		for range 4 {
			wait.Go(func() {
				if _, err := store.AppendDSHTranscript(ctx, scope, "root", []DSHTranscriptEvent{next}); err != nil {
					t.Error(err)
				}
			})
		}
		wait.Wait()
		transcript, events, err := store.ReadDSHTranscript(ctx, scope, "root", 0, 200)
		if err != nil || transcript.NextSeq != 2 || len(events) != 2 {
			t.Fatalf("concurrent=%+v,%d,%v", transcript, len(events), err)
		}
	})
	t.Run("two hundred rows bind JSON and duplicate prefixes only insert their suffix", func(t *testing.T) {
		batch := dshBatchEvents(0, 200)
		batch[50].Payload = []byte(`{"seq":50,"text":"中文\\path\"quote","nested":{"items":[1,2,3]}}`)
		result, err := store.AppendDSHTranscript(ctx, scope, "batch", batch)
		if err != nil || result.NextSeq != 200 {
			t.Fatalf("batch=%+v error=%v", result, err)
		}
		_, events, err := store.ReadDSHTranscript(ctx, scope, "batch", 0, 200)
		if err != nil || len(events) != 200 {
			t.Fatalf("read events=%d error=%v", len(events), err)
		}
		for index, event := range events {
			equal, err := jsoncanonical.Equal(batch[index].Payload, event.Payload)
			if err != nil || !equal || event.Seq != int64(index) {
				t.Fatalf("row %d seq=%d equal=%v error=%v", index, event.Seq, equal, err)
			}
		}
		if result, err = store.AppendDSHTranscript(ctx, scope, "batch", batch); err != nil || result.NextSeq != 200 {
			t.Fatalf("duplicate batch=%+v error=%v", result, err)
		}
		mixed := dshBatchEvents(198, 4)
		mixed[0].Payload = []byte(`{"value":1.0,"seq":198}`)
		if result, err = store.AppendDSHTranscript(ctx, scope, "batch", mixed); err != nil || result.NextSeq != 202 {
			t.Fatalf("mixed batch=%+v error=%v", result, err)
		}
		transcript, tail, err := store.ReadDSHTranscript(ctx, scope, "batch", 198, 200)
		if err != nil || transcript.NextSeq != 202 || len(tail) != 4 {
			t.Fatalf("mixed read=%+v events=%d error=%v", transcript, len(tail), err)
		}
	})
	t.Run("concurrent duplicate batches remain two hundred rows", func(t *testing.T) {
		if _, err := store.PutDSHTranscript(ctx, scope, "batch-concurrent", nil, []byte(`{"version":4,"id":"batch-concurrent","createdAt":0,"isSeeded":false}`)); err != nil {
			t.Fatal(err)
		}
		batch := dshBatchEvents(0, 200)
		var wait sync.WaitGroup
		for range 4 {
			wait.Go(func() {
				if result, err := store.AppendDSHTranscript(ctx, scope, "batch-concurrent", batch); err != nil || result.NextSeq != 200 {
					t.Errorf("concurrent batch=%+v error=%v", result, err)
				}
			})
		}
		wait.Wait()
		transcript, events, err := store.ReadDSHTranscript(ctx, scope, "batch-concurrent", 0, 200)
		if err != nil || transcript.NextSeq != 200 || len(events) != 200 {
			t.Fatalf("concurrent transcript=%+v events=%d error=%v", transcript, len(events), err)
		}
	})
}

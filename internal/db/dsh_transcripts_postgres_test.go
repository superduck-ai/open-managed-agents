//go:build integration

package db

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"

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

	header := DSHTranscriptRecord{Offset: 0, Payload: []byte(` {"version":99,"unknown":9007199254740993} `)}
	first := DSHTranscriptRecord{Offset: 1, Payload: []byte(`{"seq":0,"future":"中文"}`)}
	t.Run("missing header", func(t *testing.T) {
		if _, err := store.AppendDSHTranscript(ctx, scope, "root", nil, []DSHTranscriptRecord{first}); !errors.Is(err, ErrDSHHeaderRequired) {
			t.Fatal(err)
		}
	})
	t.Run("wrong owner", func(t *testing.T) {
		old := scope
		old.Epoch--
		if _, err := store.AppendDSHTranscript(ctx, old, "root", nil, []DSHTranscriptRecord{header}); !errors.Is(err, ErrWorkerEpochMismatch) {
			t.Fatal(err)
		}
	})
	t.Run("initial gap rolls back creation", func(t *testing.T) {
		if _, err := store.AppendDSHTranscript(ctx, scope, "root", nil, []DSHTranscriptRecord{header, {Offset: 2, Payload: []byte(`{}`)}}); !errors.Is(err, ErrInvalidState) {
			t.Fatal(err)
		}
		if _, _, err := store.ReadDSHTranscript(ctx, scope, "root", 0, 200, 4<<20); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
	if result, err := store.AppendDSHTranscript(ctx, scope, "root", nil, []DSHTranscriptRecord{header, first}); err != nil || result.NextOffset != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	t.Run("byte conflict rejects semantic duplicate and suffix", func(t *testing.T) {
		for _, record := range []DSHTranscriptRecord{
			{Offset: 0, Payload: []byte(`{"version":99,"unknown":9007199254740993}`)},
			{Offset: 1, Payload: []byte(`{"future":"中文","seq":0}`)},
		} {
			if _, err := store.AppendDSHTranscript(ctx, scope, "root", nil, []DSHTranscriptRecord{record}); !errors.Is(err, ErrDSHTranscriptConflict) {
				t.Fatal(err)
			}
		}
		if result, err := store.AppendDSHTranscript(ctx, scope, "root", nil, []DSHTranscriptRecord{{Offset: 3, Payload: []byte(`{}`)}}); !errors.Is(err, ErrDSHTranscriptGap) || result.NextOffset != 2 {
			t.Fatalf("%+v %v", result, err)
		}
	})
	t.Run("tenant isolation", func(t *testing.T) {
		other := scope
		other.WorkspaceUUID = "53000000-0000-0000-0000-000000000099"
		if _, _, err := store.ReadDSHTranscript(ctx, other, "root", 0, 200, 4<<20); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("database unique constraint", func(t *testing.T) {
		mapper := NewDSHTranscriptMapper(store.mapperDB)
		row, found, err := mapper.Find(ctx, scope, "root")
		if err != nil || !found {
			t.Fatal(err)
		}
		if _, err := mapper.InsertRecords(ctx, dshTranscriptRecordsInsert{Scope: scope, TranscriptUUID: row.UUID, Records: []DSHTranscriptRecord{first}}); err == nil {
			t.Fatal("duplicate offset accepted")
		}
	})
	t.Run("raw bytes survive retries and read", func(t *testing.T) {
		if result, err := store.AppendDSHTranscript(ctx, scope, "root", nil, []DSHTranscriptRecord{header, first}); err != nil || result.NextOffset != 2 {
			t.Fatalf("%+v %v", result, err)
		}
		transcript, records, err := store.ReadDSHTranscript(ctx, scope, "root", 0, 200, 4<<20)
		if err != nil || transcript.NextOffset != 2 || len(records) != 2 || !bytes.Equal(records[0].Payload, header.Payload) || !bytes.Equal(records[1].Payload, first.Payload) {
			t.Fatalf("%+v %+v %v", transcript, records, err)
		}
	})
	t.Run("concurrent duplicate batches and suffix", func(t *testing.T) {
		batch := append([]DSHTranscriptRecord{header, first}, dshBatchRecords(2, 198)...)
		var wait sync.WaitGroup
		for range 4 {
			wait.Go(func() {
				if result, err := store.AppendDSHTranscript(ctx, scope, "root", nil, batch); err != nil || result.NextOffset != 200 {
					t.Errorf("%+v %v", result, err)
				}
			})
		}
		wait.Wait()
		transcript, records, err := store.ReadDSHTranscript(ctx, scope, "root", 0, 200, 4<<20)
		if err != nil || transcript.NextOffset != 200 || len(records) != 200 {
			t.Fatalf("%+v %d %v", transcript, len(records), err)
		}
		if result, err := store.AppendDSHTranscript(ctx, scope, "root", nil, dshBatchRecords(198, 4)); err != nil || result.NextOffset != 202 {
			t.Fatalf("%+v %v", result, err)
		}
	})
	t.Run("byte budget stops before exceeding but returns an oversized first record", func(t *testing.T) {
		_, all, err := store.ReadDSHTranscript(ctx, scope, "root", 2, 200, 4<<20)
		if err != nil || len(all) < 3 {
			t.Fatalf("%d %v", len(all), err)
		}
		budget := int64(len(all[0].Payload) + len(all[1].Payload) + 1)
		_, page, err := store.ReadDSHTranscript(ctx, scope, "root", 2, 200, budget)
		if err != nil || len(page) != 3 || page[0].Offset != 2 || page[2].Offset != 4 {
			t.Fatalf("budget=%d records=%+v err=%v", budget, page, err)
		}
		_, page, err = store.ReadDSHTranscript(ctx, scope, "root", 2, 200, 1)
		if err != nil || len(page) != 1 || !bytes.Equal(page[0].Payload, all[0].Payload) {
			t.Fatalf("records=%+v err=%v", page, err)
		}
		_, page, err = store.ReadDSHTranscript(ctx, scope, "root", 202, 200, 1)
		if err != nil || len(page) != 0 {
			t.Fatalf("records=%+v err=%v", page, err)
		}
	})
}

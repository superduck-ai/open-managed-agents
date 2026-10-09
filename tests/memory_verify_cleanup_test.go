package tests

import (
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
)

func TestVerifyMemoryCleanup(t *testing.T) {
	started := time.Now()
	t.Run("store deletion requires durable cleanup", func(t *testing.T) {
		f := newMemoryVerification(t)
		f.started = started
		memoryVerifyWrite(t, f.fx, f.path("/retained.md"), "retained\n")
		restore := f.rejectCleanupQueue(t)
		f.objects.failDelete.Store(true)
		memoryVerifyResponse(t, doMemoryRequest(t, f.app, "DELETE", "/v1/memory_stores/"+f.fx.store.ID+"?beta=true", nil, defaultTestKey, true), 500)
		memoryVerifyRead(t, f.fx, f.path("/retained.md"), "retained\n")
		assertPayloadSQLCount(t, f.app, "select count(*) from jobs where type='object_cleanup'", 0)
		restore()
		f.objects.failDelete.Store(false)
		deleteMemoryStore(t, f.app, f.fx.store.ID)
		f.assertObjectAbsent(t, f.objects.keys[0])
		f.proof(t, "deletion_cleanup_atomic")
	})
	t.Run("redaction requires durable cleanup", func(t *testing.T) {
		f := newMemoryVerification(t)
		f.started = started
		seed := createMemory(t, f.app, f.fx.store.ID, "/retained.md", "first\n")
		memoryVerifyWrite(t, f.fx, f.path("/retained.md"), "second\n")
		restore := f.rejectCleanupQueue(t)
		f.objects.failDelete.Store(true)
		memoryVerifyResponse(t, doMemoryRequest(t, f.app, "POST", "/v1/memory_stores/"+f.fx.store.ID+"/memory_versions/"+seed.MemoryVersionID+"/redact?beta=true", nil, defaultTestKey, true), 500)
		first := retrieveMemoryVersion(t, f.app, f.fx.store.ID, seed.MemoryVersionID)
		if first.RedactedAt != nil || first.Content == nil || *first.Content != "first\n" {
			t.Fatal("failed cleanup enqueue redacted source")
		}
		restore()
		f.objects.failDelete.Store(false)
		redacted := redactMemoryVersion(t, f.app, f.fx.store.ID, seed.MemoryVersionID)
		if redacted.RedactedAt == nil || redacted.Content != nil {
			t.Fatal("version redaction did not remove content")
		}
		f.assertObjectAbsent(t, f.objects.keys[0])
		memoryVerifyRead(t, f.fx, f.path("/retained.md"), "second\n")
	})
	t.Run("rejected writes preserve head", func(t *testing.T) {
		f := newMemoryVerification(t)
		f.started = started
		memoryVerifyWrite(t, f.fx, f.path("/notes.md"), "before\n")
		f.objects.failUpload.Store(true)
		memoryVerifyResponse(t, f.fx.createFile(t, f.path("/notes.md"), []byte("lost")), 503)
		f.objects.failUpload.Store(false)
		memoryVerifyRead(t, f.fx, f.path("/notes.md"), "before\n")
		if len(listMemoryVersions(t, f.app, f.fx.store.ID, "").Data) != 1 {
			t.Fatal("failed write created version")
		}
		memoryVerifyResponse(t, f.fx.createFile(t, f.path("/notes.md/child"), []byte("orphan")), 409)
		if len(f.objects.keys) != 2 {
			t.Fatalf("expected successful and rejected metadata uploads, got %d", len(f.objects.keys))
		}
		f.assertObjectAbsent(t, f.objects.keys[1])
		f.proof(t, "failed_write_compensated")
	})
	t.Run("metadata rejection cleanup retries", func(t *testing.T) {
		f := newMemoryVerification(t)
		f.started = started
		memoryVerifyWrite(t, f.fx, f.path("/parent"), "retained\n")
		f.objects.failDelete.Store(true)
		memoryVerifyResponse(t, f.fx.createFile(t, f.path("/parent/child"), []byte("orphan")), 409)
		assertPayloadSQLCount(t, f.app, "select count(*) from jobs where type='object_cleanup' and status='pending'", 1)
		f.objects.failDelete.Store(false)
		worker := cleanup.NewWorker(f.app.db, memoryVerifyClient{f.objects}, time.Second, nil)
		if err := worker.RunOnce(t.Context(), "verify-memory-metadata"); err != nil {
			t.Fatal(err)
		}
		f.assertObjectAbsent(t, f.objects.keys[1])
		f.assertObject(t, f.objects.keys[0], "retained\n")
	})
	t.Run("cleanup enqueue failure is not success", func(t *testing.T) {
		f := newMemoryVerification(t)
		f.started = started
		memoryVerifyWrite(t, f.fx, f.path("/notes.md"), "retained\n")
		restore := f.rejectCleanupQueue(t)
		f.objects.failDelete.Store(true)
		memoryVerifyResponse(t, f.fx.createFile(t, f.path("/notes.md"), []byte("retained\n")), 500)
		f.objects.failDelete.Store(false)
		restore()
		memoryVerifyRead(t, f.fx, f.path("/notes.md"), "retained\n")
	})
	t.Run("identical flush cleanup retries", func(t *testing.T) {
		f := newMemoryVerification(t)
		f.started = started
		memoryVerifyWrite(t, f.fx, f.path("/notes.md"), "retained\n")
		f.objects.failDelete.Store(true)
		memoryVerifyWrite(t, f.fx, f.path("/notes.md"), "retained\n")
		assertPayloadSQLCount(t, f.app, "select count(*) from jobs where type='object_cleanup' and status='pending'", 1)
		f.objects.failDelete.Store(false)
		worker := cleanup.NewWorker(f.app.db, memoryVerifyClient{f.objects}, time.Second, nil)
		if err := worker.RunOnce(t.Context(), "verify-memory-flush"); err != nil {
			t.Fatal(err)
		}
		f.assertObjectAbsent(t, f.objects.keys[1])
		f.assertObject(t, f.objects.keys[0], "retained\n")
		if len(listMemoryVersions(t, f.app, f.fx.store.ID, "").Data) != 1 {
			t.Fatal("cleanup changed memory versions")
		}
		f.proof(t, "discarded_upload_cleanup_recovered")
	})
	for _, retry := range []bool{false, true} {
		name := "direct store deletion"
		if retry {
			name = "queued deletion retries"
		}
		t.Run(name, func(t *testing.T) {
			f := newMemoryVerification(t)
			f.started = started
			memoryVerifyWrite(t, f.fx, f.path("/notes.md"), "first\n")
			memoryVerifyWrite(t, f.fx, f.path("/notes.md"), "second\n")
			f.objects.failDelete.Store(retry)
			deleteMemoryStore(t, f.app, f.fx.store.ID)
			memoryVerifyResponse(t, f.fx.json(t, "readFile", map[string]any{"filesystemId": f.fx.filesystem.ExternalID, "path": f.path("/notes.md")}), 404)
			if retry {
				worker := cleanup.NewWorker(f.app.db, memoryVerifyClient{f.objects}, time.Second, nil)
				if err := worker.RunOnce(t.Context(), "verify-memory-failed"); err != nil {
					t.Fatal(err)
				}
				assertPayloadSQLCount(t, f.app, "select count(*) from jobs where type='object_cleanup' and status='retry' and attempts=1", 2)
				for index, key := range f.objects.keys {
					f.assertObject(t, key, []string{"first\n", "second\n"}[index])
				}
				f.proof(t, "cleanup_retry_persisted")
				f.objects.failDelete.Store(false)
				if _, err := f.app.pool.Exec(t.Context(), "update jobs set run_after=now() where type='object_cleanup' and status='retry'"); err != nil {
					t.Fatal(err)
				}
				if err := worker.RunOnce(t.Context(), "verify-memory-recovery"); err != nil {
					t.Fatal(err)
				}
				assertPayloadSQLCount(t, f.app, "select count(*) from jobs where type='object_cleanup' and status='completed'", 2)
				if err := worker.RunOnce(t.Context(), "verify-memory-idempotency"); err != nil {
					t.Fatal(err)
				}
			}
			for _, key := range f.objects.keys {
				f.assertObjectAbsent(t, key)
			}
			if !retry {
				f.proof(t, "memory_objects_removed")
			}
		})
	}
}

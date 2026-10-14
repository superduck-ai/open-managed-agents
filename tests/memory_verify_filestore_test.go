package tests

import (
	"fmt"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

func TestVerifyMemoryFilestore(t *testing.T) {
	f := newMemoryVerification(t)
	memoryVerifyWrite(t, f.fx, f.path("/keep.md"), "persistent memory\n")
	memoryKey := headObjectKey(t, f, findMemoryID(t, f, "/keep.md"))
	memoryVerifyWrite(t, f.fx, "/outputs/a.txt", "original\n")
	memoryVerifyResponse(t, f.fx.createFileWithParams(t, "/outputs/a.txt", []byte("deny"), map[string]any{"overwriteExisting": false}), 409)
	memoryVerifyRead(t, f.fx, "/outputs/a.txt", "original\n")
	memoryVerifyWrite(t, f.fx, "/outputs/a.txt", "updated\n中文")
	memoryVerifyRead(t, f.fx, "/outputs/a.txt", "updated\n中文")
	memoryVerifyResponse(t, f.fx.json(t, "copyFile", map[string]any{"filesystemId": f.fx.filesystem.ExternalID, "source": "/outputs/a.txt", "destination": "/outputs/b.txt"}), 200)
	memoryVerifyRead(t, f.fx, "/outputs/b.txt", "updated\n中文")
	memoryVerifyResponse(t, f.fx.json(t, "moveFile", map[string]any{"filesystemId": f.fx.filesystem.ExternalID, "source": "/outputs/b.txt", "destination": "/outputs/c.txt"}), 200)
	memoryVerifyRead(t, f.fx, "/outputs/c.txt", "updated\n中文")
	memoryVerifyResponse(t, f.fx.json(t, "readFile", map[string]any{"filesystemId": f.fx.filesystem.ExternalID, "path": "/outputs/b.txt"}), 404)
	memoryVerifyResponse(t, f.fx.json(t, "removeFile", map[string]any{"filesystemId": f.fx.filesystem.ExternalID, "path": "/outputs/c.txt"}), 200)
	memoryVerifyResponse(t, f.fx.json(t, "readFile", map[string]any{"filesystemId": f.fx.filesystem.ExternalID, "path": "/outputs/c.txt"}), 404)
	f.proof(t, "filestore_mutations_match")
	for i := 0; i < 101; i++ {
		memoryVerifyWrite(t, f.fx, fmt.Sprintf("/outputs/batch-%03d.txt", i), "batch\n")
	}
	deleteSession(t, f.app, f.fx.session.ID)
	worker := filestore.NewCleanupWorker(f.app.db, memoryVerifyClient{f.objects}, nil)
	if _, err := f.app.pool.Exec(t.Context(), "update jobs set run_after=now() where type='filestore_object_cleanup' and status='pending' and payload->>'reason'='orphan_guard'"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if err := worker.RunFilesystemCleanupOnce(t.Context(), "verify-memory-filesystem"); err != nil {
			t.Fatal(err)
		}
		if err := worker.RunCleanupOnce(t.Context(), "verify-memory-objects"); err != nil {
			t.Fatal(err)
		}
	}
	assertPayloadSQLCount(t, f.app, "select count(*) from jobs where type in ('filestore_filesystem_cleanup','filestore_object_cleanup') and status not in ('completed','canceled')", 0)
	assertPayloadSQLCount(t, f.app, "select coalesce(sum(files_bytes+filestore_bytes),0) from workspace_storage_usage", 0)
	for _, key := range f.objects.keys {
		if key != memoryKey {
			f.assertObjectAbsent(t, key)
		}
	}
	f.proof(t, "session_owned_cleanup_completed")
	f.assertObject(t, memoryKey, "persistent memory\n")
	b := f.attach(t, "read_only")
	memoryVerifyRead(t, b, f.path("/keep.md"), "persistent memory\n")
	f.proof(t, "memory_survives_filesystem_cleanup")
}

func findMemoryID(t *testing.T, f *memoryVerification, path string) string {
	t.Helper()
	head, found := findMemoryByPath(t, f.app, f.fx.store.ID, path)
	if !found {
		t.Fatalf("memory %s missing", path)
	}
	return head.ID
}

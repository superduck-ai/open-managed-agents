package tests

import (
	"encoding/json"
	"testing"
)

func TestVerifyMemoryLifecycle(t *testing.T) {
	f := newMemoryVerification(t)
	seed := createMemory(t, f.app, f.fx.store.ID, "/notes/preferences.md", "seed\n中文\n")
	b := f.attach(t, "read_write")
	memoryVerifyRead(t, f.fx, f.path("/notes/preferences.md"), "seed\n中文\n")
	memoryVerifyWrite(t, f.fx, f.path("/notes/preferences.md"), "session A\n中文\n")
	memoryVerifyRead(t, b, f.path("/notes/preferences.md"), "session A\n中文\n")
	head := retrieveMemoryFull(t, f.app, f.fx.store.ID, seed.ID)
	assertMemoryContent(t, head, "session A\n中文\n")
	versions := listMemoryVersions(t, f.app, f.fx.store.ID, "memory_id="+seed.ID+"&limit=20")
	if len(versions.Data) != 2 {
		t.Fatalf("versions=%d want=2", len(versions.Data))
	}
	version := retrieveMemoryVersion(t, f.app, f.fx.store.ID, head.MemoryVersionID)
	if version.CreatedBy.Type != "session_actor" || version.CreatedBy.SessionID != f.fx.session.ID || version.Content == nil || *version.Content != "session A\n中文\n" {
		t.Fatalf("wrong version content/actor: %+v", version)
	}
	memoryVerifyWrite(t, f.fx, f.path("/notes/preferences.md"), "session A\n中文\n")
	if len(listMemoryVersions(t, f.app, f.fx.store.ID, "memory_id="+seed.ID+"&limit=20").Data) != 2 {
		t.Fatal("identical flush created a version")
	}
	for _, key := range f.objects.keys {
		if key != headObjectKey(t, f, seed.ID) && key != versionObjectKey(t, f, seed.MemoryVersionID) {
			f.assertObjectAbsent(t, key)
		}
	}
	f.proof(t, "memory_versions_match")
	deleteSession(t, f.app, f.fx.session.ID)
	memoryVerifyRead(t, b, f.path("/notes/preferences.md"), "session A\n中文\n")
	memoryVerifyWrite(t, b, f.path("/notes/preferences.md"), "session B\n")
	c := f.attach(t, "read_only")
	memoryVerifyRead(t, c, f.path("/notes/preferences.md"), "session B\n")
	old := retrieveMemoryVersion(t, f.app, f.fx.store.ID, seed.MemoryVersionID)
	if old.Content == nil || *old.Content != "seed\n中文\n" {
		t.Fatal("historical seed version changed")
	}
	f.proof(t, "cross_session_memory_preserved")
	memoryVerifyResponse(t, b.json(t, "removeFile", map[string]any{"filesystemId": b.filesystem.ExternalID, "path": f.path("/notes/preferences.md")}), 200)
	memoryVerifyResponse(t, c.json(t, "readFile", map[string]any{"filesystemId": c.filesystem.ExternalID, "path": f.path("/notes/preferences.md")}), 404)
	if _, found := findMemoryByPath(t, f.app, f.fx.store.ID, "/notes/preferences.md"); found {
		t.Fatal("deleted memory still listed")
	}
	history := listMemoryVersions(t, f.app, f.fx.store.ID, "memory_id="+seed.ID+"&limit=20")
	if len(history.Data) != 4 {
		data, _ := json.Marshal(history)
		t.Fatalf("delete history=%s", data)
	}
	for _, v := range history.Data {
		loaded := retrieveMemoryVersion(t, f.app, f.fx.store.ID, v.ID)
		if loaded.ID != v.ID {
			t.Fatal("historical version unreadable")
		}
	}
	f.proof(t, "deleted_memory_history_preserved")
}

func headObjectKey(t *testing.T, f *memoryVerification, id string) string {
	t.Helper()
	record, err := f.app.db.GetMemory(t.Context(), f.fx.workspaceUUID, f.fx.store.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return record.S3Key
}

func versionObjectKey(t *testing.T, f *memoryVerification, id string) string {
	t.Helper()
	record, err := f.app.db.GetMemoryVersion(t.Context(), f.fx.workspaceUUID, f.fx.store.ID, id)
	if err != nil || record.S3Key == nil {
		t.Fatalf("version object: %v", err)
	}
	return *record.S3Key
}

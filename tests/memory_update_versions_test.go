//go:build integration || e2e || e2b_integration

package tests

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestMemoryUpdateAlwaysCreatesVersion(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("memory-bucket"))
	defer app.close()

	t.Run("failure stale precondition with identical content", func(t *testing.T) {
		store := createMemoryStore(t, app, "memory-update-stale")
		defer deleteMemoryStore(t, app, store.ID)
		memory := createMemory(t, app, store.ID, "/same.md", "unchanged")
		before := retrieveMemoryVersion(t, app, store.ID, memory.MemoryVersionID)
		body := `{"content":"unchanged","precondition":{"type":"content_sha256","content_sha256":"` + strings.Repeat("0", 64) + `"}}`
		response := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+store.ID+"/memories/"+memory.ID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, response, http.StatusConflict, "memory_precondition_failed_error")
		current := retrieveMemory(t, app, store.ID, memory.ID, defaultTestKey)
		assertMemoryContent(t, current, "unchanged")
		versions := listMemoryVersions(t, app, store.ID, "memory_id="+url.QueryEscape(memory.ID))
		if current.MemoryVersionID != memory.MemoryVersionID || len(versions.Data) != 1 || !reflect.DeepEqual(retrieveMemoryVersion(t, app, store.ID, memory.MemoryVersionID), before) {
			t.Fatal("rejected identical-content update changed head or history")
		}
	})

	t.Run("success identical and omitted values retain every snapshot", func(t *testing.T) {
		store := createMemoryStore(t, app, "memory-update-versions")
		defer deleteMemoryStore(t, app, store.ID)
		memory := createMemory(t, app, store.ID, "/same.md", "unchanged")
		snapshots := map[string]memoryVersionAPIResponse{memory.MemoryVersionID: retrieveMemoryVersion(t, app, store.ID, memory.MemoryVersionID)}
		bodies := []string{
			`{"content":"unchanged"}`,
			`{"path":"/same.md"}`,
			`{"path":"/same.md","content":"unchanged"}`,
			`{}`,
			`{"content":"unchanged","precondition":{"type":"content_sha256","content_sha256":"` + memory.ContentSHA256 + `"}}`,
		}
		for i, body := range bodies {
			updated := updateMemory(t, app, store.ID, memory.ID, body)
			if updated.ID != memory.ID || updated.Path != memory.Path || updated.MemoryVersionID == memory.MemoryVersionID || snapshots[updated.MemoryVersionID].ID != "" {
				t.Fatalf("update %d did not create a fresh version of the same Memory: %+v", i, updated)
			}
			current := retrieveMemory(t, app, store.ID, memory.ID, defaultTestKey)
			assertMemoryContent(t, current, "unchanged")
			if current.MemoryVersionID != updated.MemoryVersionID {
				t.Fatal("retrieved Memory head differs from update response")
			}
			versions := listMemoryVersions(t, app, store.ID, "memory_id="+url.QueryEscape(memory.ID)+"&limit=20")
			if len(versions.Data) != i+2 {
				t.Fatalf("update %d version count = %d, want %d", i, len(versions.Data), i+2)
			}
			for id, before := range snapshots {
				if !reflect.DeepEqual(retrieveMemoryVersion(t, app, store.ID, id), before) {
					t.Fatalf("update %d changed old snapshot %s", i, id)
				}
			}
			version := retrieveMemoryVersion(t, app, store.ID, updated.MemoryVersionID)
			if version.Operation != "modified" || version.Content == nil || *version.Content != "unchanged" || version.ContentSHA256 == nil || *version.ContentSHA256 != memory.ContentSHA256 {
				t.Fatalf("unexpected modified snapshot: %+v", version)
			}
			snapshots[version.ID] = version
			memory = current
		}
	})
}

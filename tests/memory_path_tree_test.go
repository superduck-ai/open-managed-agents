package tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestMemoryPathTreeConflicts(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("memory-path-tree"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"path-tree"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"path-tree"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	for _, operation := range []string{"createFile", "copyFile", "moveFile", "restCreate", "restRename"} {
		for _, destination := range []string{"/notes", "/blocked/child.md", "/draft.md/child"} {
			t.Run(operation+destination, func(t *testing.T) {
				fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "path-tree-store", "")
				defer fx.cleanup()
				child := createMemory(t, app, fx.store.ID, "/notes/a.md", "child")
				blocked := createMemory(t, app, fx.store.ID, "/blocked", "file")
				source := createMemory(t, app, fx.store.ID, "/draft.md", "draft")
				root := "/memory/" + fx.slug()
				var resp *http.Response
				switch operation {
				case "createFile":
					resp = fx.createFile(t, root+destination, []byte("conflict"))
				case "copyFile", "moveFile":
					resp = fx.json(t, operation, map[string]any{"filesystemId": fx.filesystem.ExternalID, "source": root + source.Path, "destination": root + destination})
				default:
					endpoint := "/v1/memory_stores/" + fx.store.ID + "/memories"
					if operation == "restRename" {
						endpoint += "/" + source.ID
					}
					resp = doMemoryRequest(t, app, http.MethodPost, endpoint+"?beta=true", strings.NewReader(`{"path":`+quoteJSON(destination)+`,"content":"conflict"}`), defaultTestKey, true)
				}
				if strings.HasPrefix(operation, "rest") {
					conflictID := child.ID
					if destination == "/blocked/child.md" {
						conflictID = blocked.ID
					}
					if destination == "/draft.md/child" {
						conflictID = source.ID
					}
					assertMemoryPathConflict(t, resp, conflictID, destination)
				} else {
					assertFilestoreError(t, resp, http.StatusConflict, "already_exists")
				}
				for _, original := range []memoryAPIResponse{child, blocked, source} {
					got := retrieveMemory(t, app, fx.store.ID, original.ID, defaultTestKey)
					if got.Path != original.Path || got.MemoryVersionID != original.MemoryVersionID {
						t.Fatalf("failed write changed memory: got %+v, original %+v", got, original)
					}
				}
				listed := fx.json(t, "listDirectory", map[string]any{"filesystemId": fx.filesystem.ExternalID, "path": root + "/notes"})
				defer listed.Body.Close()
				if listed.StatusCode != http.StatusOK {
					t.Fatalf("directory no longer readable: %d %s", listed.StatusCode, readAll(t, listed.Body))
				}
			})
		}
	}
	t.Run("segment boundaries and literal characters", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "literal-paths", "")
		defer fx.cleanup()
		for _, path := range []string{"/notes", "/notes-old/a.md", "/pct%", "/pctX/a.md", "/under_", "/underX/a.md", "/中文", "/中文件/a.md"} {
			createMemory(t, app, fx.store.ID, path, "ok")
		}
		deleted := createMemory(t, app, fx.store.ID, "/removed", "old")
		deleteMemory(t, app, fx.store.ID, deleted.ID, deleted.ContentSHA256)
		createMemory(t, app, fx.store.ID, "/removed/child.md", "new")
	})
}

func TestMemoryPathTreeConcurrentCreate(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("memory-tree-concurrent"))
	defer app.close()
	store := createMemoryStore(t, app, "concurrent-tree")
	defer deleteMemoryStore(t, app, store.ID)
	start := make(chan struct{})
	type outcome struct {
		response *http.Response
		err      error
	}
	results := make(chan outcome, 2)
	for _, path := range []string{"/notes", "/notes/child.md"} {
		request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/memory_stores/"+store.ID+"/memories?beta=true", strings.NewReader(`{"path":`+quoteJSON(path)+`,"content":"concurrent"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Api-Key", defaultTestKey)
		request.Header.Set("anthropic-version", "2023-06-01")
		request.Header.Set("anthropic-beta", "files-api-2025-04-14, managed-agents-2026-04-01")
		request.Header.Set("Content-Type", "application/json")
		go func() { <-start; response, err := app.client.Do(request); results <- outcome{response, err} }()
	}
	close(start)
	statuses := map[int]int{}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Error(result.err)
			continue
		}
		statuses[result.response.StatusCode]++
		result.response.Body.Close()
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusConflict] != 1 {
		t.Fatalf("concurrent parent/child writes: statuses = %v, want one success and one conflict", statuses)
	}
}

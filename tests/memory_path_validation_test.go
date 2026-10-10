package tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestMemoryPathValidationAcrossEntrypoints(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("memory-path-validation"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"path-validation"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"path-validation"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "path-validation", "")
	defer fx.cleanup()
	root := "/memory/" + fx.slug()
	for _, path := range []string{"/cafe\u0301.md", "/line\nbreak.md", "/tab\tname.md", "/zero\u200bwidth.md", "/bidi\u202ename.md"} {
		t.Run(path, func(t *testing.T) {
			resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+fx.store.ID+"/memories?beta=true", strings.NewReader(`{"path":`+quoteJSON(path)+`,"content":"invalid"}`), defaultTestKey, true)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
			assertFilestoreError(t, fx.createFile(t, root+path, []byte("invalid")), http.StatusBadRequest, "invalid_argument")
			for _, operation := range []string{"makeDirectory", "listDirectory", "removeFile", "readMetadata"} {
				assertFilestoreError(t, fx.json(t, operation, map[string]any{"filesystemId": fx.filesystem.ExternalID, "path": root + path}), http.StatusBadRequest, "invalid_argument")
			}
			if _, found := findMemoryByPath(t, app, fx.store.ID, path); found {
				t.Fatal("invalid path persisted")
			}
		})
	}
	for _, path := range []string{"/café.md", "/中文/笔记.md"} {
		t.Run(path, func(t *testing.T) {
			created := createMemory(t, app, fx.store.ID, path, "REST")
			response := fx.createFile(t, root+path, []byte("Filestore"))
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("valid path rejected: %d %s", response.StatusCode, readAll(t, response.Body))
			}
			got := retrieveMemoryFull(t, app, fx.store.ID, created.ID)
			if got.Path != path || got.Content == nil || *got.Content != "Filestore" {
				t.Fatalf("not the same memory: %+v", got)
			}
		})
	}
}

//go:build integration || e2e || e2b_integration

package tests

import (
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDeletedSessionOutputIsImmediatelyHidden(t *testing.T) {
	store := newFakeStore("workspace-storage")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"output-delete-visibility"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"output-delete-visibility"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	source := uploadFile(t, app, "retained.txt", "text/plain", []byte("uploaded"))
	defer deleteFile(t, app, source.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(t.Context(), record.WorkspaceUUID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	blob := workspaceStorageBlob(6, nil)
	blob.Downloadable = true
	store.objects[blob.S3Key] = fakeObject{data: []byte("output"), contentType: "text/plain"}
	if _, err := app.db.PutFilestoreFile(t.Context(), db.PutFilestoreFileInput{WorkspaceUUID: record.WorkspaceUUID, FilesystemUUID: filesystem.UUID, Path: "/outputs/result.txt", Blob: blob}); err != nil {
		t.Fatal(err)
	}
	page := listFiles(t, app, "scope_id="+session.ID)
	if len(page.Data) != 1 {
		t.Fatalf("outputs=%+v", page.Data)
	}
	outputID := page.Data[0].ID
	output, err := app.db.GetFile(t.Context(), record.WorkspaceUUID, outputID)
	if err != nil {
		t.Fatal(err)
	}
	archive := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/archive?beta=true", nil, defaultTestKey, true)
	if archive.StatusCode != http.StatusOK {
		t.Fatalf("archive=%d: %s", archive.StatusCode, readAll(t, archive.Body))
	}
	archive.Body.Close()
	if _, err := app.db.GetFile(t.Context(), record.WorkspaceUUID, outputID); err != nil {
		t.Fatalf("archive hid output: %v", err)
	}
	response := app.do(t, http.MethodGet, "/v1/files/"+outputID+"/content", nil, defaultTestKey, false, "")
	if response.StatusCode != http.StatusOK || string(readAll(t, response.Body)) != "output" {
		t.Fatalf("archived output download status=%d", response.StatusCode)
	}
	response.Body.Close()
	deleteSession(t, app, session.ID)
	t.Run("failure deleted output download and metadata", func(t *testing.T) {
		for _, suffix := range []string{"", "/content"} {
			response := app.do(t, http.MethodGet, "/v1/files/"+outputID+suffix, nil, defaultTestKey, false, "")
			assertError(t, response, http.StatusNotFound, "not_found_error")
		}
	})
	t.Run("failure deleted output absent from file catalogs", func(t *testing.T) {
		for _, scope := range []string{"", "scope_id=" + session.ID} {
			page := listFiles(t, app, scope)
			if containsFile(page.Data, outputID) {
				t.Fatal("deleted output still listed")
			}
		}
		files, err := app.db.ListFiles(t.Context(), record.WorkspaceUUID, session.ID)
		if err != nil || len(files) != 0 {
			t.Fatalf("unpaged catalog=%+v error=%v", files, err)
		}
	})
	t.Run("success uploaded source and deferred cleanup preserved", func(t *testing.T) {
		if _, err := app.db.GetFile(t.Context(), record.WorkspaceUUID, source.ID); err != nil {
			t.Fatalf("uploaded source hidden: %v", err)
		}
		if _, err := app.db.GetFileByUUID(t.Context(), record.WorkspaceUUID, output.UUID); err != nil {
			t.Fatalf("metadata purged synchronously: %v", err)
		}
		if _, ok := store.objects[blob.S3Key]; !ok {
			t.Fatal("object deleted synchronously")
		}
		if _, err := app.db.GetFilestoreFilesystemBySession(t.Context(), record.WorkspaceUUID, session.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("filesystem still visible: %v", err)
		}
	})
}

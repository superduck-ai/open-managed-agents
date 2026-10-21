//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionInputFileInstances(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("input-instances-bucket"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"input-instances-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"input-instances-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	source := uploadFile(t, app, "shared.txt", "text/plain", []byte("shared input"))
	defer deleteFile(t, app, source.ID)
	if _, err := app.pool.Exec(t.Context(), `UPDATE files SET downloadable=true WHERE external_id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	source.Downloadable = true
	before := defaultWorkspaceStorageBytes(t, app)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`,"resources":[{"type":"file","file_id":`+quoteJSON(source.ID)+`,"mount_path":"/one"},{"type":"file","file_id":`+quoteJSON(source.ID)+`,"mount_path":"/two"}]}`)
	sessionDeleted := false
	defer func() {
		if !sessionDeleted {
			deleteSession(t, app, session.ID)
		}
	}()
	resources := make([]struct {
		ID     string `json:"id"`
		FileID string `json:"file_id"`
	}, len(session.Resources))
	for i, raw := range session.Resources {
		if err := json.Unmarshal(raw, &resources[i]); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("failure mounted source and instances cannot be deleted", func(t *testing.T) {
		for _, id := range []string{source.ID, resources[0].FileID, resources[1].FileID} {
			response := app.do(t, http.MethodDelete, "/v1/files/"+id, nil, defaultTestKey, false, "")
			assertError(t, response, http.StatusConflict, "conflict_error")
		}
	})
	t.Run("failure duplicate mount leaves no orphan instance", func(t *testing.T) {
		record := mustSessionRecord(t, app, session.ID)
		before, err := app.db.ListFiles(t.Context(), record.WorkspaceUUID, session.ID)
		if err != nil {
			t.Fatal(err)
		}
		response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/resources?beta=true",
			strings.NewReader(`{"type":"file","file_id":`+quoteJSON(source.ID)+`,"mount_path":"/one"}`), defaultTestKey, true)
		assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		after, err := app.db.ListFiles(t.Context(), record.WorkspaceUUID, session.ID)
		if err != nil || len(after) != len(before) {
			t.Fatalf("failed mount changed instances: %+v, %v", after, err)
		}
		var count int
		if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM files WHERE workspace_uuid=$1 AND scope_id=$2`, record.WorkspaceUUID, session.ID).Scan(&count); err != nil || count != len(before) {
			t.Fatalf("failed mount left instance metadata: %d, %v", count, err)
		}
	})

	t.Run("success each mount has a stable File identity without storage charge", func(t *testing.T) {
		if resources[0].FileID == "" || resources[0].FileID == resources[1].FileID || resources[0].FileID == source.ID || resources[1].FileID == source.ID {
			t.Fatalf("input instances = %+v, source=%s", resources, source.ID)
		}
		page := listFiles(t, app, "scope_id="+session.ID)
		if len(page.Data) != 2 || !containsFile(page.Data, resources[0].FileID) || !containsFile(page.Data, resources[1].FileID) || containsFile(page.Data, source.ID) {
			t.Fatalf("input catalog=%+v", page.Data)
		}
		for _, file := range page.Data {
			if file.Filename != source.Filename || file.SizeBytes != source.SizeBytes || file.Downloadable != source.Downloadable {
				t.Fatalf("instance metadata=%+v, source=%+v", file, source)
			}
		}
		for _, resource := range resources {
			response := app.do(t, http.MethodGet, "/v1/files/"+resource.FileID, nil, defaultTestKey, false, "")
			if response.StatusCode != http.StatusOK {
				t.Fatalf("instance metadata status=%d: %s", response.StatusCode, readAll(t, response.Body))
			}
			var metadata struct {
				ID    string `json:"id"`
				Scope struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"scope"`
			}
			decodeJSON(t, response.Body, &metadata)
			response.Body.Close()
			if metadata.ID != resource.FileID || metadata.Scope.ID != session.ID || metadata.Scope.Type != "session" {
				t.Fatalf("instance scope=%+v", metadata)
			}
			download := app.do(t, http.MethodGet, "/v1/files/"+resource.FileID+"/content", nil, defaultTestKey, false, "")
			if download.StatusCode != http.StatusOK || string(readAll(t, download.Body)) != "shared input" {
				t.Fatalf("instance download status=%d", download.StatusCode)
			}
			download.Body.Close()
		}
		if got := defaultWorkspaceStorageBytes(t, app); got != before {
			t.Fatalf("instance storage=%d want %d", got, before)
		}
		record := mustSessionRecord(t, app, session.ID)
		reconciled, err := app.db.ReconcileWorkspaceStorageUsage(context.Background(), record.WorkspaceUUID)
		if err != nil || reconciled != before {
			t.Fatalf("reconciled storage=%d, %v want %d", reconciled, err, before)
		}
		again := retrieveSession(t, app, session.ID, defaultTestKey)
		for _, raw := range again.Resources {
			var resource struct {
				FileID string `json:"file_id"`
			}
			if err := json.Unmarshal(raw, &resource); err != nil {
				t.Fatal(err)
			}
			if resource.FileID != resources[0].FileID && resource.FileID != resources[1].FileID {
				t.Fatalf("instance identity changed: %s", raw)
			}
		}
	})
	child := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`,"resources":[{"type":"file","file_id":`+quoteJSON(resources[0].FileID)+`,"mount_path":"/child"}]}`)
	childDeleted := false
	defer func() {
		if !childDeleted {
			deleteSession(t, app, child.ID)
		}
	}()
	var childResource struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(child.Resources[0], &childResource); err != nil {
		t.Fatal(err)
	}
	if childResource.FileID == resources[0].FileID || childResource.FileID == source.ID {
		t.Fatalf("child did not create independent instance: %+v", childResource)
	}
	t.Run("success detach retires only that instance", func(t *testing.T) {
		response := doSessionRequest(t, app, http.MethodDelete, "/v1/sessions/"+session.ID+"/resources/"+resources[0].ID+"?beta=true", nil, defaultTestKey, true)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("detach status %d", response.StatusCode)
		}
		deleted := app.do(t, http.MethodGet, "/v1/files/"+resources[0].FileID, nil, defaultTestKey, false, "")
		assertError(t, deleted, http.StatusNotFound, "not_found_error")
		page := listFiles(t, app, "scope_id="+session.ID)
		if len(page.Data) != 1 || page.Data[0].ID != resources[1].FileID {
			t.Fatalf("remaining instances=%+v", page.Data)
		}
	})
	deleteSession(t, app, session.ID)
	sessionDeleted = true
	record := mustSessionRecord(t, app, child.ID)
	if _, err := app.db.GetFile(context.Background(), record.WorkspaceUUID, resources[1].FileID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("session-deleted instance error=%v", err)
	}
	page := listFiles(t, app, "scope_id="+child.ID)
	if len(page.Data) != 1 || page.Data[0].ID != childResource.FileID {
		t.Fatalf("child instance lost after parent deletion: %+v", page.Data)
	}
	rejected := app.do(t, http.MethodDelete, "/v1/files/"+source.ID, nil, defaultTestKey, false, "")
	assertError(t, rejected, http.StatusConflict, "conflict_error")
	deleteSession(t, app, child.ID)
	childDeleted = true
	if got := defaultWorkspaceStorageBytes(t, app); got != before {
		t.Fatalf("retirement changed storage: %d want %d", got, before)
	}
}

func TestSessionMountedOutputSourceIsProtected(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("mounted-output-source-bucket"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"mounted-output-source-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"mounted-output-source-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	parent := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, parent.ID)
	record := mustSessionRecord(t, app, parent.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), record.WorkspaceUUID, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := db.PutFilestoreFileInput{WorkspaceUUID: record.WorkspaceUUID, FilesystemUUID: filesystem.UUID, Path: "/outputs/source.txt", Blob: workspaceStorageBlob(7, nil)}
	if _, err := app.db.PutFilestoreFile(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	files := listFiles(t, app, "scope_id="+parent.ID)
	if len(files.Data) != 1 {
		t.Fatalf("source output=%+v", files.Data)
	}
	child := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`,"resources":[{"type":"file","file_id":`+quoteJSON(files.Data[0].ID)+`,"mount_path":"/snapshot"}]}`)
	childDeleted := false
	defer func() {
		if !childDeleted {
			deleteSession(t, app, child.ID)
		}
	}()
	input.OverwriteExisting = true
	input.Blob = workspaceStorageBlob(8, nil)
	if _, err := app.db.PutFilestoreFile(context.Background(), input); !errors.Is(err, db.ErrFileInUse) {
		t.Fatalf("overwrite mounted source error=%v", err)
	}
	removal := db.RemoveSessionResourceFileInput{WorkspaceUUID: record.WorkspaceUUID, FilesystemUUID: filesystem.UUID, Path: input.Path}
	if _, err := app.db.RemoveFilestoreFile(context.Background(), removal); !errors.Is(err, db.ErrFileInUse) {
		t.Fatalf("remove mounted source error=%v", err)
	}
	deleteSession(t, app, child.ID)
	childDeleted = true
	if _, err := app.db.RemoveFilestoreFile(context.Background(), removal); err != nil {
		t.Fatalf("remove source after unmount: %v", err)
	}
}

func TestSessionMountedOutputCleanupWaitsWithoutExhaustingRetries(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("mounted-source-cleanup"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"source-cleanup"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"source-cleanup"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	parent := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	parentDeleted := false
	defer func() {
		if !parentDeleted {
			deleteSession(t, app, parent.ID)
		}
	}()
	record := mustSessionRecord(t, app, parent.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(t.Context(), record.WorkspaceUUID, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.PutFilestoreFile(t.Context(), db.PutFilestoreFileInput{
		WorkspaceUUID: record.WorkspaceUUID, FilesystemUUID: filesystem.UUID, Path: "/outputs/source.txt", Blob: workspaceStorageBlob(7, nil),
	}); err != nil {
		t.Fatal(err)
	}
	source := listFiles(t, app, "scope_id="+parent.ID).Data[0]
	sourceRecord, err := app.db.GetFile(t.Context(), record.WorkspaceUUID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	child := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`,"resources":[{"type":"file","file_id":`+quoteJSON(source.ID)+`,"mount_path":"/source.txt"}]}`)
	childDeleted := false
	defer func() {
		if !childDeleted {
			deleteSession(t, app, child.ID)
		}
	}()
	deleteSession(t, app, parent.ID)
	parentDeleted = true
	process := func() bool {
		t.Helper()
		var jobUUID string
		if err := app.pool.QueryRow(t.Context(), `UPDATE jobs SET status='running', locked_by='input-source-cleanup',locked_until=now()+interval '1 minute'
   WHERE type='filestore_filesystem_cleanup' AND payload->>'filesystem_uuid'=$1 RETURNING uuid::text`, filesystem.UUID).Scan(&jobUUID); err != nil {
			t.Fatal(err)
		}
		done, anomalies, err := app.db.ProcessLeasedFilestoreFilesystemCleanupJob(t.Context(), jobUUID, "input-source-cleanup", 100)
		if err != nil || len(anomalies) != 0 {
			t.Fatalf("cleanup error=%v anomalies=%+v", err, anomalies)
		}
		var status string
		var attempts int
		if err := app.pool.QueryRow(t.Context(), `SELECT status,attempts FROM jobs WHERE uuid=$1`, jobUUID).Scan(&status, &attempts); err != nil {
			t.Fatal(err)
		}
		if attempts != 0 || (!done && status != "pending") {
			t.Fatalf("reference wait consumed retry budget: %s, %d", status, attempts)
		}
		return done
	}
	for range 12 {
		if process() {
			t.Fatal("cleanup completed while source was mounted")
		}
	}
	if _, err := app.db.GetFile(t.Context(), record.WorkspaceUUID, source.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("deleted session source remains public: %v", err)
	}
	if _, err := app.db.GetFileByUUID(t.Context(), record.WorkspaceUUID, sourceRecord.UUID); err != nil {
		t.Fatalf("waiting cleanup retired source metadata: %v", err)
	}
	var objectJobs int
	if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE type='filestore_object_cleanup' AND payload->>'filesystem_uuid'=$1`, filesystem.UUID).Scan(&objectJobs); err != nil || objectJobs != 0 {
		t.Fatalf("waiting source has object jobs: %d, %v", objectJobs, err)
	}
	deleteSession(t, app, child.ID)
	childDeleted = true
	if !process() {
		t.Fatal("cleanup did not resume after final reference retired")
	}
	if _, err := app.db.GetFile(t.Context(), record.WorkspaceUUID, source.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("cleaned source still visible: %v", err)
	}
	if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE type='filestore_object_cleanup' AND payload->>'filesystem_uuid'=$1`, filesystem.UUID).Scan(&objectJobs); err != nil || objectJobs != 1 {
		t.Fatalf("released source object jobs: %d, %v", objectJobs, err)
	}
}

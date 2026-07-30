package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionFileResourceContract(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-file-resources-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-file-resource-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-file-resource-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	file := uploadFile(t, app, "quarterly report.csv", "text/csv", []byte("quarter,total\nQ1,10\n"))
	defer deleteFile(t, app, file.ID)

	base := `"agent":` + quoteJSON(agent.ID) + `,"environment_id":` + quoteJSON(env.ID)
	assertNoResources := func(t *testing.T, sessionExternalID string) {
		t.Helper()
		session := mustSessionRecord(t, app, sessionExternalID)
		resources, err := app.db.ListSessionResources(
			context.Background(),
			session.WorkspaceUUID,
			session.ExternalID,
		)
		if err != nil {
			t.Fatalf("list Session resources: %v", err)
		}
		if len(resources) != 0 {
			t.Fatalf("Session resources = %+v, want none", resources)
		}
	}

	t.Run("failure create references missing file", func(t *testing.T) {
		resp := doSessionRequest(
			t,
			app,
			http.MethodPost,
			"/v1/sessions?beta=true",
			strings.NewReader(`{`+base+`,"resources":[{"type":"file","file_id":"file_missing_create"}]}`),
			defaultTestKey,
			true,
		)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure add references missing file and rolls back resource", func(t *testing.T) {
		created := createSession(t, app, `{`+base+`}`)
		defer deleteSession(t, app, created.ID)

		resp := doSessionRequest(
			t,
			app,
			http.MethodPost,
			"/v1/sessions/"+created.ID+"/resources?beta=true",
			strings.NewReader(`{"type":"file","file_id":"file_missing_add","mount_path":"/workspace/missing.txt"}`),
			defaultTestKey,
			true,
		)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
		assertNoResources(t, created.ID)
	})

	t.Run("failure occupied Filestore path rolls back resource", func(t *testing.T) {
		created := createSession(t, app, `{`+base+`}`)
		defer deleteSession(t, app, created.ID)
		session := mustSessionRecord(t, app, created.ID)
		filesystem, err := app.db.GetFilestoreFilesystemBySession(
			context.Background(),
			session.WorkspaceUUID,
			session.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem: %v", err)
		}
		if _, err := app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
			WorkspaceID:  workspaceInternalIDForUUID(t, app, session.WorkspaceUUID),
			FilesystemID: filesystem.ID,
			Path:         "/uploads/workspace",
			MakeParents:  true,
		}); err != nil {
			t.Fatalf("create occupied path parent: %v", err)
		}
		if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  workspaceInternalIDForUUID(t, app, session.WorkspaceUUID),
			FilesystemID: filesystem.ID,
			Path:         "/uploads/workspace/occupied.txt",
			Blob:         workspaceStorageBlob(0, nil),
		}); err != nil {
			t.Fatalf("create occupied Filestore path: %v", err)
		}

		resp := doSessionRequest(
			t,
			app,
			http.MethodPost,
			"/v1/sessions/"+created.ID+"/resources?beta=true",
			strings.NewReader(`{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/occupied.txt"}`),
			defaultTestKey,
			true,
		)
		assertError(t, resp, http.StatusConflict, "conflict_error")
		assertNoResources(t, created.ID)
	})

	t.Run("failure active file resource path conflicts return bad request", func(t *testing.T) {
		tests := []struct {
			name        string
			existing    string
			conflicting string
		}{
			{
				name:        "duplicate",
				existing:    "/workspace/data.csv",
				conflicting: "/workspace/data.csv",
			},
			{
				name:        "existing ancestor",
				existing:    "/workspace/repository",
				conflicting: "/workspace/repository/data.csv",
			},
			{
				name:        "existing descendant",
				existing:    "/workspace/repository/data.csv",
				conflicting: "/workspace/repository",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				created := createSession(
					t,
					app,
					`{`+base+`,"resources":[{"type":"file","file_id":`+
						quoteJSON(file.ID)+`,"mount_path":`+quoteJSON(test.existing)+`}]}`,
				)
				defer deleteSession(t, app, created.ID)

				resp := doSessionRequest(
					t,
					app,
					http.MethodPost,
					"/v1/sessions/"+created.ID+"/resources?beta=true",
					strings.NewReader(
						`{"type":"file","file_id":`+quoteJSON(file.ID)+
							`,"mount_path":`+quoteJSON(test.conflicting)+`}`,
					),
					defaultTestKey,
					true,
				)
				assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

				session := mustSessionRecord(t, app, created.ID)
				resources, err := app.db.ListSessionResources(
					context.Background(),
					session.WorkspaceUUID,
					session.ExternalID,
				)
				if err != nil {
					t.Fatalf("list resources after path conflict: %v", err)
				}
				if len(resources) != 1 {
					t.Fatalf("resources after path conflict = %d, want 1", len(resources))
				}
			})
		}
	})

	for _, test := range []struct {
		name     string
		resource string
	}{
		{name: "another source", resource: `{"type":"file","file_id":` + quoteJSON(file.ID) + `,"source":"/outputs","mount_path":"/workspace/data.csv"}`},
		{name: "null source", resource: `{"type":"file","file_id":` + quoteJSON(file.ID) + `,"source":null,"mount_path":"/workspace/data.csv"}`},
		{name: "relative path", resource: `{"type":"file","file_id":` + quoteJSON(file.ID) + `,"mount_path":"workspace/data.csv"}`},
		{name: "path traversal", resource: `{"type":"file","file_id":` + quoteJSON(file.ID) + `,"mount_path":"/workspace/../etc/passwd"}`},
	} {
		t.Run("failure "+test.name, func(t *testing.T) {
			resp := doSessionRequest(
				t,
				app,
				http.MethodPost,
				"/v1/sessions?beta=true",
				strings.NewReader(`{`+base+`,"resources":[`+test.resource+`]}`),
				defaultTestKey,
				true,
			)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		})
	}

	t.Run("failure duplicate and file ancestor paths", func(t *testing.T) {
		for _, resources := range []string{
			`[
				{"type":"file","file_id":` + quoteJSON(file.ID) + `,"mount_path":"/workspace/data.csv"},
				{"type":"file","file_id":` + quoteJSON(file.ID) + `,"mount_path":"/workspace/data.csv"}
			]`,
			`[
				{"type":"file","file_id":` + quoteJSON(file.ID) + `,"mount_path":"/workspace/repository"},
				{"type":"file","file_id":` + quoteJSON(file.ID) + `,"mount_path":"/workspace/repository/data.csv"}
			]`,
		} {
			resp := doSessionRequest(
				t,
				app,
				http.MethodPost,
				"/v1/sessions?beta=true",
				strings.NewReader(`{`+base+`,"resources":`+resources+`}`),
				defaultTestKey,
				true,
			)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		}
	})

	t.Run("failure more than 100 files", func(t *testing.T) {
		resources := make([]string, 0, 101)
		for index := 0; index < 101; index++ {
			resources = append(resources, `{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/files/data-`+strconv.Itoa(index)+`.csv"}`)
		}
		resp := doSessionRequest(
			t,
			app,
			http.MethodPost,
			"/v1/sessions?beta=true",
			strings.NewReader(`{`+base+`,"resources":[`+strings.Join(resources, ",")+`]}`),
			defaultTestKey,
			true,
		)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success defaults and add resource use uploads", func(t *testing.T) {
		expectedBytes := defaultWorkspaceStorageBytes(t, app)
		created := createSession(t, app, `{`+base+`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`}]}`)
		defer deleteSession(t, app, created.ID)
		if len(created.Resources) != 1 {
			t.Fatalf("created resources = %d, want 1", len(created.Resources))
		}
		assertFileResourcePayload(t, created.Resources[0], file.ID, "/uploads", "/"+file.ID)
		assertSessionFileReference(
			t,
			app,
			created.ID,
			created.Resources[0],
			file.ID,
			"/uploads/"+file.ID,
		)
		scopedFiles := listFiles(t, app, "scope_id="+created.ID)
		if len(scopedFiles.Data) != 1 {
			t.Fatalf("scoped files after create = %+v, want one input projection", scopedFiles.Data)
		}
		if scopedFiles.Data[0].ID == file.ID || scopedFiles.Data[0].Filename != file.Filename {
			t.Fatalf(
				"input projection = %+v, want a new file ID with filename %q",
				scopedFiles.Data[0],
				file.Filename,
			)
		}
		if scopedFiles.Data[0].Downloadable {
			t.Fatalf("input projection = %+v, want source download policy preserved", scopedFiles.Data[0])
		}
		inputDownload := app.do(
			t,
			http.MethodGet,
			"/v1/files/"+scopedFiles.Data[0].ID+"/content?beta=true",
			nil,
			defaultTestKey,
			true,
			"",
		)
		assertError(t, inputDownload, http.StatusBadRequest, "invalid_request_error")

		resp := doSessionRequest(
			t,
			app,
			http.MethodPost,
			"/v1/sessions/"+created.ID+"/resources?beta=true",
			strings.NewReader(`{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/data.csv"}`),
			defaultTestKey,
			true,
		)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("add file resource status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var added json.RawMessage
		decodeJSON(t, resp.Body, &added)
		assertFileResourcePayload(t, added, file.ID, "/uploads", "/workspace/data.csv")
		addedResourceID := assertSessionFileReference(
			t,
			app,
			created.ID,
			added,
			file.ID,
			"/uploads/workspace/data.csv",
		)
		scopedFiles = listFiles(t, app, "scope_id="+created.ID)
		if len(scopedFiles.Data) != 2 {
			t.Fatalf("scoped files after add = %+v, want two input projections", scopedFiles.Data)
		}
		if scopedFiles.Data[0].ID == scopedFiles.Data[1].ID {
			t.Fatalf("input projections reused file ID: %+v", scopedFiles.Data)
		}
		sessionRecord := mustSessionRecord(t, app, created.ID)
		if _, err := app.db.Pool.Exec(context.Background(), `
				update workspace_storage_usage
			set filestore_bytes = 123
			where workspace_uuid = $1
		`, sessionRecord.WorkspaceUUID); err != nil {
			t.Fatalf("introduce storage ledger drift: %v", err)
		}
		reconciledBytes, err := app.db.ReconcileWorkspaceStorageUsage(
			context.Background(),
			sessionRecord.WorkspaceUUID,
		)
		if err != nil {
			t.Fatalf("reconcile workspace storage usage: %v", err)
		}
		if reconciledBytes != expectedBytes {
			t.Fatalf(
				"reconciled workspace storage bytes = %d, want physical bytes %d",
				reconciledBytes,
				expectedBytes,
			)
		}

		conflict := doSessionRequest(
			t,
			app,
			http.MethodPost,
			"/v1/sessions/"+created.ID+"/resources?beta=true",
			strings.NewReader(`{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/data.csv/child"}`),
			defaultTestKey,
			true,
		)
		assertError(t, conflict, http.StatusBadRequest, "invalid_request_error")

		deleted := doSessionRequest(
			t,
			app,
			http.MethodDelete,
			"/v1/sessions/"+created.ID+"/resources/"+addedResourceID+"?beta=true",
			nil,
			defaultTestKey,
			true,
		)
		defer deleted.Body.Close()
		if deleted.StatusCode != http.StatusOK {
			t.Fatalf("delete file resource status = %d: %s", deleted.StatusCode, readAll(t, deleted.Body))
		}
		session := sessionRecord
		filesystem, err := app.db.GetFilestoreFilesystemBySession(
			context.Background(),
			session.WorkspaceUUID,
			session.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem after resource delete: %v", err)
		}
		if _, err := app.db.GetFilestoreEntry(
			context.Background(),
			workspaceInternalIDForUUID(t, app, session.WorkspaceUUID),
			filesystem.ID,
			"/uploads/workspace/data.csv",
		); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("deleted file resource entry error = %v, want ErrNotFound", err)
		}
		parent, err := app.db.GetFilestoreEntry(
			context.Background(),
			workspaceInternalIDForUUID(t, app, session.WorkspaceUUID),
			filesystem.ID,
			"/uploads/workspace",
		)
		if err != nil {
			t.Fatalf("resource delete pruned the database-maintained parent directory: %v", err)
		}
		if parent.Kind != db.FilestoreEntryKindDirectory {
			t.Fatalf("resource parent kind = %q, want directory", parent.Kind)
		}
		if _, err := app.db.GetFile(context.Background(), session.WorkspaceUUID, file.ID); err != nil {
			t.Fatalf("source File was changed by resource delete: %v", err)
		}
		scopedFiles = listFiles(t, app, "scope_id="+created.ID)
		if len(scopedFiles.Data) != 1 {
			t.Fatalf("scoped files after resource delete = %+v, want one remaining input", scopedFiles.Data)
		}
	})

	t.Run("success file paths are isolated beneath uploads", func(t *testing.T) {
		created := createSession(t, app, `{`+base+`,"resources":[
			{"type":"github_repository","url":"https://github.com/example/repository","mount_path":"/workspace/repository"},
			{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/repository/data.csv"},
			{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/tmp/rclone-mount-config.json"}
		]}`)
		defer deleteSession(t, app, created.ID)
		if len(created.Resources) != 3 {
			t.Fatalf("created resources = %d, want 3", len(created.Resources))
		}
	})

	t.Run("success deleting session removes scoped projections", func(t *testing.T) {
		created := createSession(t, app, `{`+base+`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`}]}`)
		deleted := false
		t.Cleanup(func() {
			if !deleted {
				deleteSession(t, app, created.ID)
			}
		})
		scoped := listFiles(t, app, "scope_id="+created.ID)
		if len(scoped.Data) != 1 {
			t.Fatalf("scoped files before Session delete = %+v, want one input", scoped.Data)
		}
		session := mustSessionRecord(t, app, created.ID)
		projection, err := app.db.GetFile(context.Background(), session.WorkspaceUUID, scoped.Data[0].ID)
		if err != nil {
			t.Fatalf("load scoped projection before Session delete: %v", err)
		}
		filesystem, err := app.db.GetFilestoreFilesystemBySession(
			context.Background(),
			session.WorkspaceUUID,
			session.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem before hard-deleting backing entry: %v", err)
		}
		if _, err := app.db.Pool.Exec(context.Background(), `
			delete from filestore_entries
			where workspace_uuid = $1
				and uuid = $2
		`, filesystem.WorkspaceUUID, projection.UUID); err != nil {
			t.Fatalf("hard-delete backing entry before Session delete: %v", err)
		}
		deleteSession(t, app, created.ID)
		deleted = true
		if scoped = listFiles(t, app, "scope_id="+created.ID); len(scoped.Data) != 0 {
			t.Fatalf("scoped files after Session delete = %+v, want none", scoped.Data)
		}
	})
}

func TestSessionInputProjectionPreservesSourcePolicy(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-input-projection-policy-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-input-projection-policy-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-input-projection-policy-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	file := uploadFile(t, app, "private-input.txt", "text/plain", []byte("private input"))
	defer deleteFile(t, app, file.ID)
	if _, err := app.db.Pool.Exec(context.Background(), `
		update files
		set downloadable = false
		where external_id = $1 and deleted_at is null
	`, file.ID); err != nil {
		t.Fatalf("mark source file non-downloadable: %v", err)
	}

	session := createSession(
		t,
		app,
		`{"agent":`+quoteJSON(agent.ID)+
			`,"environment_id":`+quoteJSON(env.ID)+
			`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+
			`,"mount_path":"/private-input.txt"}]}`,
	)
	defer deleteSession(t, app, session.ID)
	scopedFiles := listFiles(t, app, "scope_id="+session.ID)
	if len(scopedFiles.Data) != 1 || scopedFiles.Data[0].Downloadable {
		t.Fatalf("input projection = %+v, want one non-downloadable file", scopedFiles.Data)
	}
}

func TestSessionFileResourceProtectsSourceFile(t *testing.T) {
	store := newFakeStore("sessions-file-reference-lifecycle-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-file-reference-lifecycle-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-file-reference-lifecycle-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	file := uploadFile(t, app, "protected.txt", "text/plain", []byte("shared object"))
	beforeSessionStorageBytes := defaultWorkspaceStorageBytes(t, app)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+
		`,"environment_id":`+quoteJSON(env.ID)+
		`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+
		`,"mount_path":"/workspace/protected.txt"}]}`)
	afterSessionStorageBytes := defaultWorkspaceStorageBytes(t, app)
	if afterSessionStorageBytes != beforeSessionStorageBytes {
		t.Fatalf(
			"storage after borrowed reference bind = %d, want unchanged %d",
			afterSessionStorageBytes,
			beforeSessionStorageBytes,
		)
	}
	sessionDeleted := false
	fileDeleted := false
	defer func() {
		if !sessionDeleted {
			deleteSession(t, app, session.ID)
		}
		if !fileDeleted {
			deleteFile(t, app, file.ID)
		}
	}()

	resourceID := assertSessionFileReference(
		t,
		app,
		session.ID,
		session.Resources[0],
		file.ID,
		"/uploads/workspace/protected.txt",
	)
	scopedFiles := listFiles(t, app, "scope_id="+session.ID)
	if len(scopedFiles.Data) != 1 {
		t.Fatalf("scoped files = %+v, want one input projection", scopedFiles.Data)
	}
	rejectedProjectionDelete := app.do(
		t,
		http.MethodDelete,
		"/v1/files/"+scopedFiles.Data[0].ID+"?beta=true",
		nil,
		defaultTestKey,
		true,
		"",
	)
	assertError(t, rejectedProjectionDelete, http.StatusConflict, "conflict_error")
	sessionRecord := mustSessionRecord(t, app, session.ID)
	fileRecord, err := app.db.GetFile(
		context.Background(),
		sessionRecord.WorkspaceUUID,
		file.ID,
	)
	if err != nil {
		t.Fatalf("load protected File: %v", err)
	}

	t.Run("failure borrowed entry cannot be copied as Filestore-owned data", func(t *testing.T) {
		filesystem, err := app.db.GetFilestoreFilesystemBySession(
			context.Background(),
			sessionRecord.WorkspaceUUID,
			sessionRecord.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem: %v", err)
		}
		beforeStorageBytes, err := app.db.GetWorkspaceStorageBytes(
			context.Background(),
			sessionRecord.WorkspaceUUID,
		)
		if err != nil {
			t.Fatalf("load storage ledger before rejected copy: %v", err)
		}

		_, err = app.db.CopyFilestoreFile(context.Background(), db.CopyFilestoreFileInput{
			WorkspaceID:         workspaceInternalIDForUUID(t, app, sessionRecord.WorkspaceUUID),
			FilesystemID:        filesystem.ID,
			SourcePath:          "/uploads/workspace/protected.txt",
			DestinationPath:     "/outputs/copied.txt",
			DestinationS3Bucket: "borrowed-copy-must-not-commit",
			DestinationS3Key:    "borrowed-copy-must-not-commit",
		})
		if !errors.Is(err, db.ErrPreconditionFailed) {
			t.Fatalf("CopyFilestoreFile() error = %v, want ErrPreconditionFailed", err)
		}

		afterStorageBytes, err := app.db.GetWorkspaceStorageBytes(
			context.Background(),
			sessionRecord.WorkspaceUUID,
		)
		if err != nil {
			t.Fatalf("load storage ledger after rejected copy: %v", err)
		}
		if afterStorageBytes != beforeStorageBytes {
			t.Fatalf(
				"storage ledger changed after rejected copy: before %d after %d",
				beforeStorageBytes,
				afterStorageBytes,
			)
		}
		if _, err := app.db.GetFilestoreEntry(
			context.Background(),
			workspaceInternalIDForUUID(t, app, sessionRecord.WorkspaceUUID),
			filesystem.ID,
			"/outputs/copied.txt",
		); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("rejected copy destination error = %v, want ErrNotFound", err)
		}
	})

	rejected := app.do(
		t,
		http.MethodDelete,
		"/v1/files/"+file.ID+"?beta=true",
		nil,
		defaultTestKey,
		true,
		"",
	)
	assertError(t, rejected, http.StatusConflict, "conflict_error")
	if _, err := app.db.GetFile(
		context.Background(),
		sessionRecord.WorkspaceUUID,
		file.ID,
	); err != nil {
		t.Fatalf("rejected delete changed source File: %v", err)
	}
	if _, exists := store.objects[fileRecord.S3Key]; !exists {
		t.Fatal("rejected delete removed the shared source object")
	}

	deletedResource := doSessionRequest(
		t,
		app,
		http.MethodDelete,
		"/v1/sessions/"+session.ID+"/resources/"+resourceID+"?beta=true",
		nil,
		defaultTestKey,
		true,
	)
	defer deletedResource.Body.Close()
	if deletedResource.StatusCode != http.StatusOK {
		t.Fatalf(
			"delete file resource status = %d: %s",
			deletedResource.StatusCode,
			readAll(t, deletedResource.Body),
		)
	}

	deletedFile := app.do(
		t,
		http.MethodDelete,
		"/v1/files/"+file.ID+"?beta=true",
		nil,
		defaultTestKey,
		true,
		"",
	)
	defer deletedFile.Body.Close()
	if deletedFile.StatusCode != http.StatusOK {
		t.Fatalf("delete unreferenced File status = %d: %s", deletedFile.StatusCode, readAll(t, deletedFile.Body))
	}
	fileDeleted = true
	if _, exists := store.objects[fileRecord.S3Key]; exists {
		t.Fatal("unreferenced File delete kept the source object")
	}

	deleteSession(t, app, session.ID)
	sessionDeleted = true
}

func TestSessionFileProjectionWorkspaceIsolation(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-file-projection-isolation-bucket"))
	defer app.close()

	otherKey := "sk-ant-session-file-projection-other"
	seedWorkspaceKey(
		t,
		app.db,
		"org_session_file_projection_other",
		"workspace_session_file_projection_other",
		"api_key_session_file_projection_other",
		otherKey,
	)

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-file-projection-isolation-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-file-projection-isolation-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	source := uploadFile(t, app, "workspace-private.txt", "text/plain", []byte("workspace private"))
	defer deleteFile(t, app, source.ID)
	session := createSession(
		t,
		app,
		`{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+
			`,"resources":[{"type":"file","file_id":`+quoteJSON(source.ID)+
			`,"mount_path":"/uploads/workspace-private.txt"}]}`,
	)
	defer deleteSession(t, app, session.ID)

	ownerPage := listFiles(t, app, "scope_id="+session.ID)
	if len(ownerPage.Data) != 1 {
		t.Fatalf("owner scoped files = %+v, want one input projection", ownerPage.Data)
	}

	otherPageResponse := app.do(
		t,
		http.MethodGet,
		"/v1/files?beta=true&scope_id="+session.ID,
		nil,
		otherKey,
		true,
		"",
	)
	defer otherPageResponse.Body.Close()
	if otherPageResponse.StatusCode != http.StatusOK {
		t.Fatalf(
			"other workspace scoped list status = %d: %s",
			otherPageResponse.StatusCode,
			readAll(t, otherPageResponse.Body),
		)
	}
	var otherPage pageResponse
	decodeJSON(t, otherPageResponse.Body, &otherPage)
	if len(otherPage.Data) != 0 {
		t.Fatalf("other workspace scoped files = %+v, want none", otherPage.Data)
	}

	otherDownload := app.do(
		t,
		http.MethodGet,
		"/v1/files/"+ownerPage.Data[0].ID+"/content?beta=true",
		nil,
		otherKey,
		true,
		"",
	)
	assertError(t, otherDownload, http.StatusNotFound, "not_found_error")
}

func TestSessionOutputProjectionWriteIsAtomic(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-output-atomic-projection-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-output-atomic-projection-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-output-atomic-projection-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		record.WorkspaceUUID,
		record.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	beforeBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
	if err != nil {
		t.Fatalf("load storage before failed output write: %v", err)
	}

	const constraint = "files_reject_session_output_projection_write_test"
	if _, err := app.db.Pool.Exec(context.Background(), `
		alter table files
		add constraint `+constraint+`
		check (scope_id is null) not valid
	`); err != nil {
		t.Fatalf("install projection failure constraint: %v", err)
	}
	defer func() {
		if _, err := app.db.Pool.Exec(
			context.Background(),
			"alter table files drop constraint if exists "+constraint,
		); err != nil {
			t.Fatalf("drop projection failure constraint: %v", err)
		}
	}()

	_, err = app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID: filesystem.ID,
		Path:         "/outputs/failed.txt",
		Blob:         workspaceStorageBlob(7, nil),
	})
	if err == nil {
		t.Fatal("output write succeeded despite projection constraint")
	}
	if _, err := app.db.GetFilestoreEntry(
		context.Background(),
		workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		filesystem.ID,
		"/outputs/failed.txt",
	); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("output entry after failed projection = %v, want ErrNotFound", err)
	}
	afterBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
	if err != nil {
		t.Fatalf("load storage after failed output write: %v", err)
	}
	if afterBytes != beforeBytes {
		t.Fatalf("storage after failed output write = %d, want %d", afterBytes, beforeBytes)
	}
	if files := listFiles(t, app, "scope_id="+session.ID); len(files.Data) != 0 {
		t.Fatalf("files after failed output write = %+v, want none", files.Data)
	}
}

func TestSessionOutputProjectionMaterializesMultipleFiles(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-output-multiple-projection-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-output-multiple-projection-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-output-multiple-projection-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		record.WorkspaceUUID,
		record.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	if _, err := app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
		WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports",
		MakeParents:  true,
	}); err != nil {
		t.Fatalf("create output directory: %v", err)
	}

	for _, output := range []struct {
		path string
		size int64
	}{
		{path: "/outputs/summary.txt", size: 7},
		{path: "/outputs/reports/details.json", size: 11},
	} {
		blob := workspaceStorageBlob(output.size, nil)
		blob.Downloadable = true
		if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			FilesystemID: filesystem.ID,
			Path:         output.path,
			Blob:         blob,
		}); err != nil {
			t.Fatalf("create output entry %s: %v", output.path, err)
		}
	}

	page := listFiles(t, app, "scope_id="+session.ID)
	if len(page.Data) != 2 {
		t.Fatalf("scoped files after multiple output writes = %+v, want two", page.Data)
	}
	byFilename := make(map[string]metadataResponse, len(page.Data))
	for _, file := range page.Data {
		byFilename[file.Filename] = file
	}
	for filename, size := range map[string]int64{
		"summary.txt":  7,
		"details.json": 11,
	} {
		file, ok := byFilename[filename]
		if !ok || file.SizeBytes != size || !file.Downloadable {
			t.Fatalf(
				"output projection %q = %+v, present=%t; want size=%d downloadable",
				filename,
				file,
				ok,
				size,
			)
		}
	}
	if _, err := app.db.RemoveFilestoreDirectory(context.Background(), db.RemoveFilestoreDirectoryInput{
		WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports",
		Recursive:    true,
	}); err != nil {
		t.Fatalf("remove output directory: %v", err)
	}
	page = listFiles(t, app, "scope_id="+session.ID)
	if len(page.Data) != 1 || page.Data[0].Filename != "summary.txt" {
		t.Fatalf("scoped files after output directory removal = %+v, want summary only", page.Data)
	}
}

func TestSessionOutputFileProjectionLifecycle(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-output-projection-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-output-projection-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-output-projection-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		record.WorkspaceUUID,
		record.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	if _, err := app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
		WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports",
		MakeParents:  true,
	}); err != nil {
		t.Fatalf("create output directory: %v", err)
	}

	firstBlob := workspaceStorageBlob(7, nil)
	firstBlob.Downloadable = true
	beforeWriteBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
	if err != nil {
		t.Fatalf("load storage before output write: %v", err)
	}
	entry, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports/result.txt",
		Blob:         firstBlob,
	})
	if err != nil {
		t.Fatalf("create output entry: %v", err)
	}
	afterWriteBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
	if err != nil {
		t.Fatalf("load storage after output write: %v", err)
	}
	if afterWriteBytes != beforeWriteBytes+firstBlob.SizeBytes {
		t.Fatalf(
			"storage after output write = %d, want %d",
			afterWriteBytes,
			beforeWriteBytes+firstBlob.SizeBytes,
		)
	}
	files, err := app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list output projections: %v", err)
	}
	if len(files) != 1 || files[0].UUID != entry.Entry.UUID || files[0].S3Key != firstBlob.S3Key ||
		files[0].Filename != "result.txt" || !files[0].Downloadable {
		t.Fatalf("output projection = %+v, want current Filestore entry", files)
	}
	projectedFileID := files[0].ExternalID

	files, err = app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list output projection again: %v", err)
	}
	if len(files) != 1 || files[0].ExternalID != projectedFileID {
		t.Fatalf("repeated output listing = %+v, want stable file ID %q", files, projectedFileID)
	}

	replacement := workspaceStorageBlob(9, nil)
	replacement.Downloadable = true
	replaced, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:       workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID:      filesystem.ID,
		Path:              "/outputs/reports/result.txt",
		Blob:              replacement,
		OverwriteExisting: true,
	})
	if err != nil {
		t.Fatalf("overwrite output entry: %v", err)
	}
	if replaced.Entry.UUID != entry.Entry.UUID {
		t.Fatalf("overwritten entry UUID = %q, want stable %q", replaced.Entry.UUID, entry.Entry.UUID)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list overwritten output projection: %v", err)
	}
	if len(files) != 1 || files[0].ExternalID != projectedFileID || files[0].S3Key != replacement.S3Key ||
		files[0].SizeBytes != replacement.SizeBytes {
		t.Fatalf("overwritten projection = %+v, want updated stable file", files)
	}

	if _, err := app.db.MoveFilestoreFile(context.Background(), db.MoveFilestoreFileInput{
		WorkspaceID:     workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID:    filesystem.ID,
		SourcePath:      "/outputs/reports/result.txt",
		DestinationPath: "/transcripts/result.txt",
	}); err != nil {
		t.Fatalf("move output outside public roots: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list projections after output move: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("projections after output moved to transcripts = %+v, want none", files)
	}

	if _, err := app.db.MoveFilestoreFile(context.Background(), db.MoveFilestoreFileInput{
		WorkspaceID:     workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID:    filesystem.ID,
		SourcePath:      "/transcripts/result.txt",
		DestinationPath: "/outputs/reports/result.txt",
	}); err != nil {
		t.Fatalf("move output back into public root: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list projections after output return: %v", err)
	}
	if len(files) != 1 || files[0].ExternalID != projectedFileID {
		t.Fatalf("projection after output return = %+v, want stable file ID %q", files, projectedFileID)
	}

	beforeRejectedDeleteBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
	if err != nil {
		t.Fatalf("load storage before rejected projection delete: %v", err)
	}
	deleteProjection := app.do(
		t,
		http.MethodDelete,
		"/v1/files/"+projectedFileID+"?beta=true",
		nil,
		defaultTestKey,
		true,
		"",
	)
	assertError(t, deleteProjection, http.StatusConflict, "conflict_error")
	afterRejectedDeleteBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
	if err != nil {
		t.Fatalf("load storage after rejected projection delete: %v", err)
	}
	if afterRejectedDeleteBytes != beforeRejectedDeleteBytes {
		t.Fatalf(
			"storage after rejected projection delete = %d, want unchanged %d",
			afterRejectedDeleteBytes,
			beforeRejectedDeleteBytes,
		)
	}

	if _, err := app.db.RemoveFilestoreFile(context.Background(), db.RemoveFilestoreEntryInput{
		WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports/result.txt",
	}); err != nil {
		t.Fatalf("remove output entry: %v", err)
	}
	afterRemovalBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
	if err != nil {
		t.Fatalf("load storage after output removal: %v", err)
	}
	if afterRemovalBytes != beforeRejectedDeleteBytes-replacement.SizeBytes {
		t.Fatalf(
			"storage after output removal = %d, want %d",
			afterRemovalBytes,
			beforeRejectedDeleteBytes-replacement.SizeBytes,
		)
	}
	if _, err := app.db.GetFile(
		context.Background(),
		record.WorkspaceUUID,
		projectedFileID,
	); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("output projection after removal = %v, want ErrNotFound", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list removed output projection: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("projections after output removal = %+v, want none", files)
	}

	alreadyExpiredAt := time.Unix(0, 0).UTC()
	alreadyExpiredBlob := workspaceStorageBlob(2, &alreadyExpiredAt)
	if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports/already-expired.txt",
		Blob:         alreadyExpiredBlob,
	}); err != nil {
		t.Fatalf("create already expired output entry: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list output projections after already expired write: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("projections after already expired write = %+v, want none", files)
	}

	expiresAt := time.Now().UTC().Add(time.Hour)
	expiringBlob := workspaceStorageBlob(3, &expiresAt)
	expiringEntry, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports/expired.txt",
		Blob:         expiringBlob,
	})
	if err != nil {
		t.Fatalf("create expiring output entry: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list expired output projection before cleanup: %v", err)
	}
	if len(files) != 1 || files[0].Filename != "expired.txt" {
		t.Fatalf("expired output projection before cleanup = %+v, want expired.txt", files)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		update filestore_entries
		set expires_at = to_timestamp(0)
		where id = $1
	`, expiringEntry.Entry.ID); err != nil {
		t.Fatalf("expire output entry before cleanup: %v", err)
	}
	if _, err := app.db.ExpireFilestoreEntries(context.Background(), 1000); err != nil {
		t.Fatalf("expire output entry: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceUUID, record.ExternalID)
	if err != nil {
		t.Fatalf("list output projections after expiry: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("output projections after expiry = %+v, want none", files)
	}
}

func TestSessionFileReferenceUsesMutableFilestoreView(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-file-logical-view-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-file-logical-view-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-file-logical-view-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	file := uploadFile(t, app, "logical-view.txt", "text/plain", []byte("shared object"))
	defer deleteFile(t, app, file.ID)

	createReference := func(t *testing.T, mountPath string) (sessionAPIResponse, db.Session, db.FilestoreFilesystem) {
		t.Helper()
		beforeStorageBytes := defaultWorkspaceStorageBytes(t, app)
		session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+
			`,"environment_id":`+quoteJSON(env.ID)+
			`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+
			`,"mount_path":`+quoteJSON(mountPath)+`}]}`)
		afterStorageBytes := defaultWorkspaceStorageBytes(t, app)
		if afterStorageBytes != beforeStorageBytes {
			t.Fatalf(
				"storage after borrowed reference bind = %d, want unchanged %d",
				afterStorageBytes,
				beforeStorageBytes,
			)
		}
		record := mustSessionRecord(t, app, session.ID)
		filesystem, err := app.db.GetFilestoreFilesystemBySession(
			context.Background(),
			record.WorkspaceUUID,
			record.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem: %v", err)
		}
		return session, record, filesystem
	}

	t.Run("move directory preserves borrowed reference identity", func(t *testing.T) {
		session, record, filesystem := createReference(t, "/move/input.txt")
		defer deleteSession(t, app, session.ID)
		resourceID := assertSessionFileReference(
			t,
			app,
			session.ID,
			session.Resources[0],
			file.ID,
			"/uploads/move/input.txt",
		)

		moved, err := app.db.MoveFilestoreDirectory(context.Background(), db.MoveFilestoreDirectoryInput{
			WorkspaceID:     workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			FilesystemID:    filesystem.ID,
			SourcePath:      "/uploads/move",
			DestinationPath: "/uploads/moved",
		})
		if err != nil {
			t.Fatalf("move directory containing borrowed reference: %v", err)
		}
		if len(moved.CleanupJobs) != 0 {
			t.Fatalf("move directory cleanup jobs = %d, want 0", len(moved.CleanupJobs))
		}
		entry, err := app.db.GetFilestoreEntry(
			context.Background(),
			workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			filesystem.ID,
			"/uploads/moved/input.txt",
		)
		if err != nil {
			t.Fatalf("load moved borrowed reference: %v", err)
		}
		if entry.SourceFileUUID == nil || entry.ManagedResourceUUID == nil {
			t.Fatalf("moved entry lost reference identity: %+v", entry)
		}

		deleted := doSessionRequest(
			t,
			app,
			http.MethodDelete,
			"/v1/sessions/"+session.ID+"/resources/"+resourceID+"?beta=true",
			nil,
			defaultTestKey,
			true,
		)
		defer deleted.Body.Close()
		if deleted.StatusCode != http.StatusOK {
			t.Fatalf("delete moved file resource status = %d: %s", deleted.StatusCode, readAll(t, deleted.Body))
		}
		if _, err := app.db.GetFilestoreEntry(
			context.Background(),
			workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			filesystem.ID,
			"/uploads/moved/input.txt",
		); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("moved reference after resource delete error = %v, want ErrNotFound", err)
		}
	})

	t.Run("move and remove borrowed file only change logical view", func(t *testing.T) {
		session, record, filesystem := createReference(t, "/file-move/input.txt")
		defer deleteSession(t, app, session.ID)
		beforeBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
		if err != nil {
			t.Fatalf("load storage before borrowed file move: %v", err)
		}

		moved, err := app.db.MoveFilestoreFile(context.Background(), db.MoveFilestoreFileInput{
			WorkspaceID:     workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			FilesystemID:    filesystem.ID,
			SourcePath:      "/uploads/file-move/input.txt",
			DestinationPath: "/uploads/file-move/renamed.txt",
		})
		if err != nil {
			t.Fatalf("move borrowed file: %v", err)
		}
		if len(moved.CleanupJobs) != 0 {
			t.Fatalf("borrowed file move cleanup jobs = %d, want 0", len(moved.CleanupJobs))
		}
		if moved.Entry.SourceFileUUID == nil || moved.Entry.ManagedResourceUUID == nil {
			t.Fatalf("moved file lost reference identity: %+v", moved.Entry)
		}

		removed, err := app.db.RemoveFilestoreFile(context.Background(), db.RemoveFilestoreEntryInput{
			WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			FilesystemID: filesystem.ID,
			Path:         "/uploads/file-move/renamed.txt",
		})
		if err != nil {
			t.Fatalf("remove borrowed file: %v", err)
		}
		if len(removed.CleanupJobs) != 0 {
			t.Fatalf("borrowed file remove cleanup jobs = %d, want 0", len(removed.CleanupJobs))
		}
		afterBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
		if err != nil {
			t.Fatalf("load storage after borrowed file remove: %v", err)
		}
		if afterBytes != beforeBytes {
			t.Fatalf("storage after borrowed file move/remove = %d, want %d", afterBytes, beforeBytes)
		}
		if _, err := app.db.GetFile(context.Background(), record.WorkspaceUUID, file.ID); err != nil {
			t.Fatalf("borrowed view move/remove changed source File: %v", err)
		}
	})

	t.Run("recursive delete cleans owned objects but only unlinks borrowed objects", func(t *testing.T) {
		session, record, filesystem := createReference(t, "/bundle/input.txt")
		defer deleteSession(t, app, session.ID)
		ownedBlob := workspaceStorageBlob(7, nil)
		if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			FilesystemID: filesystem.ID,
			Path:         "/uploads/bundle/generated.txt",
			Blob:         ownedBlob,
		}); err != nil {
			t.Fatalf("create owned file beside borrowed reference: %v", err)
		}
		beforeBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
		if err != nil {
			t.Fatalf("load storage before recursive delete: %v", err)
		}

		removed, err := app.db.RemoveFilestoreDirectory(context.Background(), db.RemoveFilestoreDirectoryInput{
			WorkspaceID:  workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			FilesystemID: filesystem.ID,
			Path:         "/uploads/bundle",
			Recursive:    true,
		})
		if err != nil {
			t.Fatalf("remove directory containing mixed ownership: %v", err)
		}
		if len(removed.CleanupJobs) != 1 || removed.CleanupJobs[0].Key != ownedBlob.S3Key {
			t.Fatalf("recursive delete cleanup jobs = %+v, want only owned object %q", removed.CleanupJobs, ownedBlob.S3Key)
		}
		afterBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
		if err != nil {
			t.Fatalf("load storage after recursive delete: %v", err)
		}
		if afterBytes != beforeBytes-ownedBlob.SizeBytes {
			t.Fatalf("storage after recursive delete = %d, want %d", afterBytes, beforeBytes-ownedBlob.SizeBytes)
		}
		if _, err := app.db.GetFile(context.Background(), record.WorkspaceUUID, file.ID); err != nil {
			t.Fatalf("recursive view delete changed source File: %v", err)
		}
	})

	t.Run("overwrite borrowed reference accounts only for replacement object", func(t *testing.T) {
		session, record, filesystem := createReference(t, "/replace/input.txt")
		defer deleteSession(t, app, session.ID)
		beforeBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
		if err != nil {
			t.Fatalf("load storage before overwrite: %v", err)
		}
		replacement := workspaceStorageBlob(9, nil)
		replaced, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:       workspaceInternalIDForUUID(t, app, record.WorkspaceUUID),
			FilesystemID:      filesystem.ID,
			Path:              "/uploads/replace/input.txt",
			Blob:              replacement,
			OverwriteExisting: true,
		})
		if err != nil {
			t.Fatalf("overwrite borrowed reference: %v", err)
		}
		if len(replaced.CleanupJobs) != 0 {
			t.Fatalf("borrowed overwrite cleanup jobs = %+v, want none", replaced.CleanupJobs)
		}
		if replaced.Entry.SourceFileUUID != nil || replaced.Entry.ManagedResourceUUID != nil {
			t.Fatalf("replacement retained borrowed ownership: %+v", replaced.Entry)
		}
		afterBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceUUID)
		if err != nil {
			t.Fatalf("load storage after overwrite: %v", err)
		}
		if afterBytes != beforeBytes+replacement.SizeBytes {
			t.Fatalf("storage after overwrite = %d, want %d", afterBytes, beforeBytes+replacement.SizeBytes)
		}
		if _, err := app.db.GetFile(context.Background(), record.WorkspaceUUID, file.ID); err != nil {
			t.Fatalf("overwrite changed source File: %v", err)
		}
	})
}

func TestSessionFileResourceBindSerializesWithSourceDelete(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-file-concurrent-delete-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-file-concurrent-delete-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-file-concurrent-delete-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	file := uploadFile(t, app, "concurrent.txt", "text/plain", []byte("serialized"))
	defer deleteFile(t, app, file.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)

	type requestResult struct {
		operation string
		status    int
		body      []byte
		err       error
	}
	send := func(operation, method, requestPath, body string, start <-chan struct{}, results chan<- requestResult) {
		request, err := http.NewRequest(method, app.baseURL+requestPath, strings.NewReader(body))
		if err != nil {
			results <- requestResult{operation: operation, err: err}
			return
		}
		request.Header.Set("X-Api-Key", defaultTestKey)
		request.Header.Set("anthropic-version", "2023-06-01")
		request.Header.Set("anthropic-beta", "managed-agents-2026-04-01,files-api-2025-04-14")
		request.Header.Set("Content-Type", "application/json")
		<-start
		response, err := app.client.Do(request)
		if err != nil {
			results <- requestResult{operation: operation, err: err}
			return
		}
		defer response.Body.Close()
		responseBody, readErr := io.ReadAll(response.Body)
		results <- requestResult{operation: operation, status: response.StatusCode, body: responseBody, err: readErr}
	}

	start := make(chan struct{})
	results := make(chan requestResult, 2)
	go send(
		"bind",
		http.MethodPost,
		"/v1/sessions/"+session.ID+"/resources?beta=true",
		`{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/concurrent.txt"}`,
		start,
		results,
	)
	go send(
		"delete",
		http.MethodDelete,
		"/v1/files/"+file.ID+"?beta=true",
		"",
		start,
		results,
	)
	close(start)

	outcomes := make(map[string]requestResult, 2)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("%s request: %v", result.operation, result.err)
		}
		outcomes[result.operation] = result
	}
	bind := outcomes["bind"]
	deleted := outcomes["delete"]
	sessionRecord := mustSessionRecord(t, app, session.ID)
	resources, err := app.db.ListSessionResources(context.Background(), sessionRecord.WorkspaceUUID, session.ID)
	if err != nil {
		t.Fatalf("list resources after concurrent mutation: %v", err)
	}

	switch {
	case bind.status == http.StatusOK && deleted.status == http.StatusConflict:
		if len(resources) != 1 {
			t.Fatalf("successful bind persisted resources = %d, want 1", len(resources))
		}
		if _, err := app.db.GetFile(context.Background(), sessionRecord.WorkspaceUUID, file.ID); err != nil {
			t.Fatalf("source file missing after bind won race: %v", err)
		}
	case bind.status == http.StatusNotFound && deleted.status == http.StatusOK:
		if len(resources) != 0 {
			t.Fatalf("rejected bind persisted resources = %d, want 0", len(resources))
		}
		if _, err := app.db.GetFile(context.Background(), sessionRecord.WorkspaceUUID, file.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("source file lookup after delete won race = %v, want ErrNotFound", err)
		}
	default:
		t.Fatalf(
			"concurrent bind/delete statuses = bind %d (%s), delete %d (%s)",
			bind.status,
			bind.body,
			deleted.status,
			deleted.body,
		)
	}
}

func TestSessionFileReferenceRetiresWithoutOwningSourceObject(t *testing.T) {
	store := newFakeStore("sessions-file-reference-retirement-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-file-reference-retirement-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-file-reference-retirement-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	file := uploadFile(t, app, "retained.txt", "text/plain", []byte("source object"))
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+
		`,"environment_id":`+quoteJSON(env.ID)+
		`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+
		`,"mount_path":"/workspace/retained.txt"}]}`)
	sessionDeleted := false
	fileDeleted := false
	defer func() {
		if !sessionDeleted {
			deleteSession(t, app, session.ID)
		}
		if !fileDeleted {
			deleteFile(t, app, file.ID)
		}
	}()

	assertSessionFileReference(
		t,
		app,
		session.ID,
		session.Resources[0],
		file.ID,
		"/uploads/workspace/retained.txt",
	)
	sessionRecord := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		sessionRecord.WorkspaceUUID,
		session.ID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	fileRecord, err := app.db.GetFile(
		context.Background(),
		sessionRecord.WorkspaceUUID,
		file.ID,
	)
	if err != nil {
		t.Fatalf("load source File: %v", err)
	}
	var filesBytesBefore, filestoreBytesBefore int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		select files_bytes, filestore_bytes
		from workspace_storage_usage
		where workspace_uuid = $1
	`, sessionRecord.WorkspaceUUID).Scan(&filesBytesBefore, &filestoreBytesBefore); err != nil {
		t.Fatalf("load storage usage before Session retirement: %v", err)
	}

	deleteSession(t, app, session.ID)
	sessionDeleted = true
	if _, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		sessionRecord.WorkspaceUUID,
		session.ID,
	); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("retired Session filesystem lookup error = %v, want ErrNotFound", err)
	}

	var cleanupJobUUID string
	if err := app.db.Pool.QueryRow(context.Background(), `
		update jobs
		set status = 'running',
			locked_by = 'borrowed-reference-retirement-test',
			locked_until = now() + interval '1 minute',
			updated_at = now()
		where id = (
			select id
			from jobs
			where type = 'filestore_filesystem_cleanup'
				and payload->>'filesystem_uuid' = $1
			order by id desc
			limit 1
		)
		returning uuid
	`, filesystem.UUID).Scan(&cleanupJobUUID); err != nil {
		t.Fatalf("lease Session filesystem cleanup: %v", err)
	}
	done, err := app.db.ProcessLeasedFilestoreFilesystemCleanupJob(
		context.Background(),
		cleanupJobUUID,
		"borrowed-reference-retirement-test",
		100,
	)
	if err != nil || !done {
		t.Fatalf("process Session filesystem cleanup = done %v, error %v", done, err)
	}

	if _, exists := store.objects[fileRecord.S3Key]; !exists {
		t.Fatal("Session filesystem cleanup deleted the borrowed source object")
	}
	var activeEntries, filestoreObjectJobs int
	var filesBytesAfter, filestoreBytesAfter int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		select
			(select count(*)
			 from filestore_entries
			 where cast(filesystem_uuid as text) = $1 and deleted_at is null),
			(select count(*)
			 from jobs
			 where type = 'filestore_object_cleanup'
				and payload->>'filesystem_uuid' = $1
				and payload->>'reason' = 'session_deleted'),
			coalesce(files_bytes, 0),
			coalesce(filestore_bytes, 0)
		from workspace_storage_usage
		where workspace_uuid = $2
	`, filesystem.UUID, sessionRecord.WorkspaceUUID).Scan(
		&activeEntries,
		&filestoreObjectJobs,
		&filesBytesAfter,
		&filestoreBytesAfter,
	); err != nil {
		t.Fatalf("load borrowed-reference cleanup state: %v", err)
	}
	if activeEntries != 0 || filestoreObjectJobs != 0 {
		t.Fatalf(
			"borrowed-reference cleanup = active entries %d, object jobs %d; want 0, 0",
			activeEntries,
			filestoreObjectJobs,
		)
	}
	if filesBytesAfter != filesBytesBefore || filestoreBytesAfter != filestoreBytesBefore {
		t.Fatalf(
			"storage usage after borrowed-reference cleanup = files %d filestore %d, want files %d filestore %d",
			filesBytesAfter,
			filestoreBytesAfter,
			filesBytesBefore,
			filestoreBytesBefore,
		)
	}

	deletedFile := app.do(
		t,
		http.MethodDelete,
		"/v1/files/"+file.ID+"?beta=true",
		nil,
		defaultTestKey,
		true,
		"",
	)
	defer deletedFile.Body.Close()
	if deletedFile.StatusCode != http.StatusOK {
		t.Fatalf("delete File after Session retirement status = %d: %s", deletedFile.StatusCode, readAll(t, deletedFile.Body))
	}
	fileDeleted = true
	if _, exists := store.objects[fileRecord.S3Key]; exists {
		t.Fatal("File delete after Session retirement kept the source object")
	}
}

func TestCreateSessionResourceFileLimitIsAtomic(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-resource-limit-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-resource-limit-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-resource-limit-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	file := uploadFile(t, app, "shared.txt", "text/plain", []byte("shared"))
	defer deleteFile(t, app, file.ID)

	resources := make([]string, 0, 99)
	for index := range 99 {
		resources = append(resources, `{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/limit/file-`+strconv.Itoa(index)+`.txt"}`)
	}
	created := createSession(
		t,
		app,
		`{"agent":`+quoteJSON(agent.ID)+
			`,"environment_id":`+quoteJSON(env.ID)+
			`,"resources":[`+strings.Join(resources, ",")+`]}`,
	)
	defer deleteSession(t, app, created.ID)

	type addResult struct {
		status int
		body   []byte
		err    error
	}
	start := make(chan struct{})
	results := make(chan addResult, 2)
	for index := range 2 {
		go func() {
			body := strings.NewReader(
				`{"type":"file","file_id":` + quoteJSON(file.ID) +
					`,"mount_path":"/limit/concurrent-` + strconv.Itoa(index) + `.txt"}`,
			)
			request, err := http.NewRequest(
				http.MethodPost,
				app.baseURL+"/v1/sessions/"+created.ID+"/resources?beta=true",
				body,
			)
			if err != nil {
				results <- addResult{err: err}
				return
			}
			request.Header.Set("X-Api-Key", defaultTestKey)
			request.Header.Set("anthropic-version", "2023-06-01")
			request.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
			request.Header.Set("Content-Type", "application/json")
			<-start
			response, err := app.client.Do(request)
			if err != nil {
				results <- addResult{err: err}
				return
			}
			defer response.Body.Close()
			responseBody, err := io.ReadAll(response.Body)
			results <- addResult{status: response.StatusCode, body: responseBody, err: err}
		}()
	}
	close(start)

	statusCounts := map[int]int{}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("add concurrent file resource: %v", result.err)
		}
		statusCounts[result.status]++
		if result.status == http.StatusBadRequest {
			var payload struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(result.body, &payload); err != nil {
				t.Fatalf("decode rejected concurrent resource: %v", err)
			}
			if payload.Error.Type != "invalid_request_error" {
				t.Fatalf("rejected concurrent resource error = %q, want invalid_request_error", payload.Error.Type)
			}
		}
	}
	if statusCounts[http.StatusOK] != 1 || statusCounts[http.StatusBadRequest] != 1 {
		t.Fatalf("concurrent add statuses = %+v, want one 200 and one 400", statusCounts)
	}

	session := mustSessionRecord(t, app, created.ID)
	persisted, err := app.db.ListSessionResources(context.Background(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatalf("list resources after concurrent add: %v", err)
	}
	if len(persisted) != 100 {
		t.Fatalf("persisted resources = %d, want 100", len(persisted))
	}
}

func assertFileResourcePayload(t *testing.T, raw json.RawMessage, fileID, source, mountPath string) {
	t.Helper()
	var payload struct {
		FileID    string `json:"file_id"`
		Source    string `json:"source"`
		MountPath string `json:"mount_path"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode file resource: %v", err)
	}
	if payload.FileID != fileID || payload.Source != source || payload.MountPath != mountPath {
		t.Fatalf("file resource = %+v, want file_id=%q source=%q mount_path=%q", payload, fileID, source, mountPath)
	}
}

func assertSessionFileReference(
	t *testing.T,
	app *testApp,
	sessionExternalID string,
	raw json.RawMessage,
	fileExternalID string,
	entryPath string,
) string {
	t.Helper()
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.ID == "" {
		t.Fatalf("decode file resource ID: payload=%s error=%v", raw, err)
	}
	session := mustSessionRecord(t, app, sessionExternalID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		session.WorkspaceUUID,
		session.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	entry, err := app.db.GetFilestoreEntry(
		context.Background(),
		workspaceInternalIDForUUID(t, app, session.WorkspaceUUID),
		filesystem.ID,
		entryPath,
	)
	if err != nil {
		t.Fatalf("load Session file reference %q: %v", entryPath, err)
	}
	file, err := app.db.GetFile(context.Background(), session.WorkspaceUUID, fileExternalID)
	if err != nil {
		t.Fatalf("load source File: %v", err)
	}
	resource, err := app.db.GetSessionResource(
		context.Background(),
		session.WorkspaceUUID,
		session.ExternalID,
		payload.ID,
	)
	if err != nil {
		t.Fatalf("load Session file resource: %v", err)
	}
	if entry.Kind != db.FilestoreEntryKindFile ||
		entry.SourceFileUUID == nil ||
		*entry.SourceFileUUID != file.UUID ||
		entry.ManagedBy == nil ||
		*entry.ManagedBy != "session_file_resource" ||
		entry.ManagedResourceUUID == nil ||
		*entry.ManagedResourceUUID != resource.UUID ||
		entry.MD5 != nil ||
		entry.ExpiresAt != nil ||
		entry.SizeBytes == nil ||
		*entry.SizeBytes != file.SizeBytes ||
		entry.SHA256 == nil ||
		*entry.SHA256 != file.SHA256 ||
		entry.S3Bucket == nil ||
		*entry.S3Bucket != file.S3Bucket ||
		entry.S3Key == nil ||
		*entry.S3Key != file.S3Key {
		t.Fatalf("Session file reference = %#v, source File = %#v", entry, file)
	}
	return payload.ID
}

func defaultWorkspaceStorageBytes(t *testing.T, app *testApp) int64 {
	t.Helper()
	apiKey, err := app.db.GetAPIKey(context.Background(), auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatalf("load default API key: %v", err)
	}
	storageBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), apiKey.WorkspaceUUID)
	if err != nil {
		t.Fatalf("load default workspace storage usage: %v", err)
	}
	return storageBytes
}

func workspaceInternalIDForUUID(t *testing.T, app *testApp, workspaceUUID string) int64 {
	t.Helper()
	var workspaceID int64
	if err := app.db.Pool.QueryRow(
		context.Background(),
		`select id from workspaces where uuid = $1`,
		workspaceUUID,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("resolve workspace internal ID for %q: %v", workspaceUUID, err)
	}
	return workspaceID
}

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
			session.WorkspaceID,
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
			session.WorkspaceID,
			session.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem: %v", err)
		}
		if _, err := app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
			WorkspaceID:  session.WorkspaceID,
			FilesystemID: filesystem.ID,
			Path:         "/uploads/workspace",
			MakeParents:  true,
		}); err != nil {
			t.Fatalf("create occupied path parent: %v", err)
		}
		if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  session.WorkspaceID,
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
					session.WorkspaceID,
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

	t.Run("success internal outputs do not consume file resource capacity", func(t *testing.T) {
		created := createSession(t, app, `{`+base+`}`)
		defer deleteSession(t, app, created.ID)
		session := mustSessionRecord(t, app, created.ID)
		filesystem, err := app.db.GetFilestoreFilesystemBySession(
			context.Background(),
			session.WorkspaceID,
			session.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem: %v", err)
		}
		for index := 0; index < db.MaxSessionFileResources; index++ {
			if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
				WorkspaceID:  session.WorkspaceID,
				FilesystemID: filesystem.ID,
				Path:         "/outputs/generated-" + strconv.Itoa(index) + ".txt",
				Blob:         workspaceStorageBlob(0, nil),
			}); err != nil {
				t.Fatalf("create internal Output %d: %v", index, err)
			}
		}

		resp := doSessionRequest(
			t,
			app,
			http.MethodPost,
			"/v1/sessions/"+created.ID+"/resources?beta=true",
			strings.NewReader(`{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/attached.csv"}`),
			defaultTestKey,
			true,
		)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("attach after internal Outputs status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("success defaults and add resource use uploads", func(t *testing.T) {
		expectedBytes := defaultWorkspaceStorageBytes(t, app)
		created := createSession(t, app, `{`+base+`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+`}]}`)
		defer deleteSession(t, app, created.ID)
		if len(created.Resources) != 1 {
			t.Fatalf("created resources = %d, want 1", len(created.Resources))
		}
		assertFileResourcePayload(t, created.Resources[0], file.ID, "/uploads", "/"+file.ID)
		createdResourceID := assertSessionFileReference(
			t,
			app,
			created.ID,
			created.Resources[0],
			file.ID,
			"/uploads/"+file.ID,
		)
		scopedFiles := listFiles(t, app, "scope_id="+created.ID)
		if len(scopedFiles.Data) != 1 {
			t.Fatalf("scoped files after create = %+v, want one input catalog", scopedFiles.Data)
		}
		if scopedFiles.Data[0].ID != file.ID || scopedFiles.Data[0].Filename != file.Filename {
			t.Fatalf(
				"input catalog = %+v, want Source File ID %q with filename %q",
				scopedFiles.Data[0],
				file.ID,
				file.Filename,
			)
		}
		if scopedFiles.Data[0].CreatedAt != file.CreatedAt {
			t.Fatalf(
				"input catalog created_at = %q, want Source File created_at %q",
				scopedFiles.Data[0].CreatedAt,
				file.CreatedAt,
			)
		}
		if scopedFiles.Data[0].Downloadable {
			t.Fatalf("input catalog = %+v, want source download policy preserved", scopedFiles.Data[0])
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
		if createdResourceID == addedResourceID ||
			!strings.HasPrefix(createdResourceID, "sesrsc_") ||
			!strings.HasPrefix(addedResourceID, "sesrsc_") {
			t.Fatalf(
				"repeated attach Resource IDs = %q and %q, want distinct sesrsc_ identities",
				createdResourceID,
				addedResourceID,
			)
		}
		scopedFiles = listFiles(t, app, "scope_id="+created.ID)
		if len(scopedFiles.Data) != 1 || scopedFiles.Data[0].ID != file.ID {
			t.Fatalf("scoped files after repeated attach = %+v, want one deduplicated Source File %q", scopedFiles.Data, file.ID)
		}
		if scopedFiles.Data[0].CreatedAt != file.CreatedAt {
			t.Fatalf(
				"repeated attach changed Source File created_at to %q, want %q",
				scopedFiles.Data[0].CreatedAt,
				file.CreatedAt,
			)
		}
		sessionRecord := mustSessionRecord(t, app, created.ID)
		if _, err := app.db.Pool.Exec(context.Background(), `
				update workspace_storage_usage
			set filestore_bytes = 123
			where workspace_id = $1
		`, sessionRecord.WorkspaceID); err != nil {
			t.Fatalf("introduce storage ledger drift: %v", err)
		}
		reconciledBytes, err := app.db.ReconcileWorkspaceStorageUsage(
			context.Background(),
			sessionRecord.WorkspaceID,
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
			session.WorkspaceID,
			session.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem after resource delete: %v", err)
		}
		if _, err := app.db.GetSessionNamespaceNode(
			context.Background(),
			session.WorkspaceID,
			filesystem.ID,
			"/uploads/workspace/data.csv",
		); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("deleted file resource entry error = %v, want ErrNotFound", err)
		}
		parent, err := app.db.GetSessionNamespaceNode(
			context.Background(),
			session.WorkspaceID,
			filesystem.ID,
			"/uploads/workspace",
		)
		if err != nil {
			t.Fatalf("resource delete pruned the database-maintained parent directory: %v", err)
		}
		if parent.Kind != db.SessionNamespaceNodeKindDirectory {
			t.Fatalf("resource parent kind = %q, want directory", parent.Kind)
		}
		if _, err := app.db.GetFile(context.Background(), session.WorkspaceID, file.ID); err != nil {
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

	t.Run("success deleting session removes scoped catalog files", func(t *testing.T) {
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
		deleteSession(t, app, created.ID)
		deleted = true
		if scoped = listFiles(t, app, "scope_id="+created.ID); len(scoped.Data) != 0 {
			t.Fatalf("scoped files after Session delete = %+v, want none", scoped.Data)
		}
	})
}

func TestSessionInputResourcePreservesSourcePolicy(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-input-catalog-policy-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-input-catalog-policy-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-input-catalog-policy-env"}`)
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
		t.Fatalf("input catalog = %+v, want one non-downloadable file", scopedFiles.Data)
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
			"storage after Input Resource bind = %d, want unchanged %d",
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
		t.Fatalf("scoped files = %+v, want one input catalog", scopedFiles.Data)
	}
	rejectedSourceDelete := app.do(
		t,
		http.MethodDelete,
		"/v1/files/"+scopedFiles.Data[0].ID+"?beta=true",
		nil,
		defaultTestKey,
		true,
		"",
	)
	assertError(t, rejectedSourceDelete, http.StatusConflict, "conflict_error")
	sessionRecord := mustSessionRecord(t, app, session.ID)
	fileRecord, err := app.db.GetFile(
		context.Background(),
		sessionRecord.WorkspaceID,
		file.ID,
	)
	if err != nil {
		t.Fatalf("load protected File: %v", err)
	}

	t.Run("failure Input Resource cannot be copied as Filestore-owned data", func(t *testing.T) {
		filesystem, err := app.db.GetFilestoreFilesystemBySession(
			context.Background(),
			sessionRecord.WorkspaceID,
			sessionRecord.ExternalID,
		)
		if err != nil {
			t.Fatalf("load Session filesystem: %v", err)
		}
		beforeStorageBytes, err := app.db.GetWorkspaceStorageBytes(
			context.Background(),
			sessionRecord.WorkspaceID,
		)
		if err != nil {
			t.Fatalf("load storage ledger before rejected copy: %v", err)
		}

		_, err = app.db.CopyFilestoreFile(context.Background(), db.CopyFilestoreFileInput{
			WorkspaceID:         sessionRecord.WorkspaceID,
			FilesystemID:        filesystem.ID,
			SourcePath:          "/uploads/workspace/protected.txt",
			DestinationPath:     "/outputs/copied.txt",
			DestinationS3Bucket: "input-resource-copy-must-not-commit",
			DestinationS3Key:    "input-resource-copy-must-not-commit",
		})
		if !errors.Is(err, db.ErrPreconditionFailed) {
			t.Fatalf("CopyFilestoreFile() error = %v, want ErrPreconditionFailed", err)
		}

		afterStorageBytes, err := app.db.GetWorkspaceStorageBytes(
			context.Background(),
			sessionRecord.WorkspaceID,
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
		if _, err := app.db.GetSessionNamespaceNode(
			context.Background(),
			sessionRecord.WorkspaceID,
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
		sessionRecord.WorkspaceID,
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

func TestSessionFileCatalogWorkspaceIsolation(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-file-catalog-isolation-bucket"))
	defer app.close()

	otherKey := "sk-ant-session-file-catalog-other"
	seedWorkspaceKey(
		t,
		app.db,
		"org_session_file_catalog_other",
		"workspace_session_file_catalog_other",
		"api_key_session_file_catalog_other",
		otherKey,
	)

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-file-catalog-isolation-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-file-catalog-isolation-env"}`)
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
		t.Fatalf("owner scoped files = %+v, want one input catalog", ownerPage.Data)
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

func TestSessionOutputCatalogWriteIsAtomic(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-output-atomic-catalog-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-output-atomic-catalog-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-output-atomic-catalog-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		record.WorkspaceID,
		record.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	beforeBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceID)
	if err != nil {
		t.Fatalf("load storage before failed output write: %v", err)
	}

	const constraint = "files_reject_session_output_catalog_write_test"
	if _, err := app.db.Pool.Exec(context.Background(), `
		alter table files
		add constraint `+constraint+`
		check (scope_id is null) not valid
	`); err != nil {
		t.Fatalf("install catalog failure constraint: %v", err)
	}
	defer func() {
		if _, err := app.db.Pool.Exec(
			context.Background(),
			"alter table files drop constraint if exists "+constraint,
		); err != nil {
			t.Fatalf("drop catalog failure constraint: %v", err)
		}
	}()

	_, err = app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  record.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         "/outputs/failed.txt",
		Blob:         workspaceStorageBlob(7, nil),
	})
	if err == nil {
		t.Fatal("output write succeeded despite catalog constraint")
	}
	if _, err := app.db.GetSessionNamespaceNode(
		context.Background(),
		record.WorkspaceID,
		filesystem.ID,
		"/outputs/failed.txt",
	); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("output entry after failed catalog = %v, want ErrNotFound", err)
	}
	afterBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceID)
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

func TestSessionOutputCatalogMaterializesMultipleFiles(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-output-multiple-catalog-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-output-multiple-catalog-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-output-multiple-catalog-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		record.WorkspaceID,
		record.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	if _, err := app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
		WorkspaceID:  record.WorkspaceID,
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
			WorkspaceID:  record.WorkspaceID,
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
				"output catalog %q = %+v, present=%t; want size=%d downloadable",
				filename,
				file,
				ok,
				size,
			)
		}
	}
	if _, err := app.db.RemoveFilestoreDirectory(context.Background(), db.RemoveFilestoreDirectoryInput{
		WorkspaceID:  record.WorkspaceID,
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

func TestSessionOutputFileLifecycle(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-output-catalog-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-output-catalog-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-output-catalog-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		record.WorkspaceID,
		record.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	if _, err := app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
		WorkspaceID:  record.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports",
		MakeParents:  true,
	}); err != nil {
		t.Fatalf("create output directory: %v", err)
	}

	firstBlob := workspaceStorageBlob(7, nil)
	firstBlob.Downloadable = true
	beforeWriteBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceID)
	if err != nil {
		t.Fatalf("load storage before output write: %v", err)
	}
	entry, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  record.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports/result.txt",
		Blob:         firstBlob,
	})
	if err != nil {
		t.Fatalf("create output entry: %v", err)
	}
	afterWriteBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceID)
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
	files, err := app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list output catalog files: %v", err)
	}
	if len(files) != 1 || files[0].S3Key != firstBlob.S3Key ||
		files[0].Filename != "result.txt" || !files[0].Downloadable {
		t.Fatalf("output catalog = %+v, want current Filestore entry", files)
	}
	outputFileID := files[0].ExternalID
	outputFileCreatedAt := files[0].CreatedAt
	outputResourceCreatedAt := entry.Node.CreatedAt
	if allFiles := listFiles(t, app, ""); !containsFile(allFiles.Data, outputFileID) {
		t.Fatalf("unscoped Files list does not contain active Output %q: %+v", outputFileID, allFiles.Data)
	}

	files, err = app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list output catalog again: %v", err)
	}
	if len(files) != 1 || files[0].ExternalID != outputFileID {
		t.Fatalf("repeated output listing = %+v, want stable file ID %q", files, outputFileID)
	}

	replacement := workspaceStorageBlob(9, nil)
	replacement.Downloadable = true
	replaced, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:       record.WorkspaceID,
		FilesystemID:      filesystem.ID,
		Path:              "/outputs/reports/result.txt",
		Blob:              replacement,
		OverwriteExisting: true,
	})
	if err != nil {
		t.Fatalf("overwrite output entry: %v", err)
	}
	if replaced.Node.UUID != entry.Node.UUID {
		t.Fatalf("overwritten entry UUID = %q, want stable %q", replaced.Node.UUID, entry.Node.UUID)
	}
	if !replaced.Node.CreatedAt.Equal(outputResourceCreatedAt) {
		t.Fatalf("overwritten Resource created_at = %s, want stable %s", replaced.Node.CreatedAt, outputResourceCreatedAt)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list overwritten output catalog: %v", err)
	}
	if len(files) != 1 || files[0].ExternalID != outputFileID || !files[0].CreatedAt.Equal(outputFileCreatedAt) || files[0].S3Key != replacement.S3Key ||
		files[0].SizeBytes != replacement.SizeBytes {
		t.Fatalf("overwritten catalog = %+v, want updated stable file", files)
	}

	if _, err := app.db.MoveFilestoreFile(context.Background(), db.MoveFilestoreFileInput{
		WorkspaceID:     record.WorkspaceID,
		FilesystemID:    filesystem.ID,
		SourcePath:      "/outputs/reports/result.txt",
		DestinationPath: "/transcripts/result.txt",
	}); err != nil {
		t.Fatalf("move output outside public roots: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list catalog files after output move: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("catalog files after output moved to transcripts = %+v, want none", files)
	}
	if allFiles := listFiles(t, app, ""); containsFile(allFiles.Data, outputFileID) {
		t.Fatalf("unscoped Files list exposed transcript File %q: %+v", outputFileID, allFiles.Data)
	}
	hiddenMetadata := app.do(
		t,
		http.MethodGet,
		"/v1/files/"+outputFileID+"?beta=true",
		nil,
		defaultTestKey,
		true,
		"",
	)
	assertError(t, hiddenMetadata, http.StatusNotFound, "not_found_error")

	if _, err := app.db.MoveFilestoreFile(context.Background(), db.MoveFilestoreFileInput{
		WorkspaceID:     record.WorkspaceID,
		FilesystemID:    filesystem.ID,
		SourcePath:      "/transcripts/result.txt",
		DestinationPath: "/outputs/reports/result.txt",
	}); err != nil {
		t.Fatalf("move output back into public root: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list catalog files after output return: %v", err)
	}
	if len(files) != 1 || files[0].ExternalID != outputFileID {
		t.Fatalf("catalog after output return = %+v, want stable file ID %q", files, outputFileID)
	}
	if allFiles := listFiles(t, app, ""); !containsFile(allFiles.Data, outputFileID) {
		t.Fatalf("unscoped Files list did not restore Output %q: %+v", outputFileID, allFiles.Data)
	}

	beforeRejectedDeleteBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceID)
	if err != nil {
		t.Fatalf("load storage before rejected catalog delete: %v", err)
	}
	deleteCatalogFile := app.do(
		t,
		http.MethodDelete,
		"/v1/files/"+outputFileID+"?beta=true",
		nil,
		defaultTestKey,
		true,
		"",
	)
	assertError(t, deleteCatalogFile, http.StatusConflict, "conflict_error")
	afterRejectedDeleteBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceID)
	if err != nil {
		t.Fatalf("load storage after rejected catalog delete: %v", err)
	}
	if afterRejectedDeleteBytes != beforeRejectedDeleteBytes {
		t.Fatalf(
			"storage after rejected catalog delete = %d, want unchanged %d",
			afterRejectedDeleteBytes,
			beforeRejectedDeleteBytes,
		)
	}

	if _, err := app.db.RemoveFilestoreFile(context.Background(), db.RemoveSessionNamespaceNodeInput{
		WorkspaceID:  record.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports/result.txt",
	}); err != nil {
		t.Fatalf("remove output entry: %v", err)
	}
	afterRemovalBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), record.WorkspaceID)
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
		record.WorkspaceID,
		outputFileID,
	); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("output catalog after removal = %v, want ErrNotFound", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list removed output catalog: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("catalog files after output removal = %+v, want none", files)
	}

	alreadyExpiredAt := time.Unix(0, 0).UTC()
	alreadyExpiredBlob := workspaceStorageBlob(2, &alreadyExpiredAt)
	if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  record.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports/already-expired.txt",
		Blob:         alreadyExpiredBlob,
	}); err != nil {
		t.Fatalf("create already expired output entry: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list output catalog files after already expired write: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("catalog files after already expired write = %+v, want none", files)
	}

	expiresAt := time.Now().UTC().Add(time.Hour)
	expiringBlob := workspaceStorageBlob(3, &expiresAt)
	expiringEntry, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  record.WorkspaceID,
		FilesystemID: filesystem.ID,
		Path:         "/outputs/reports/expired.txt",
		Blob:         expiringBlob,
	})
	if err != nil {
		t.Fatalf("create expiring output entry: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list expired output catalog before cleanup: %v", err)
	}
	if len(files) != 1 || files[0].Filename != "expired.txt" {
		t.Fatalf("expired output catalog before cleanup = %+v, want expired.txt", files)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		update session_resources
		set expires_at = to_timestamp(0)
		where id = $1
	`, expiringEntry.Node.ID); err != nil {
		t.Fatalf("expire output entry before cleanup: %v", err)
	}
	if _, _, err := app.db.ExpireSessionNamespaceNodes(context.Background(), 1000); err != nil {
		t.Fatalf("expire output entry: %v", err)
	}
	files, err = app.db.ListFiles(context.Background(), record.WorkspaceID, record.ExternalID)
	if err != nil {
		t.Fatalf("list output catalog files after expiry: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("output catalog files after expiry = %+v, want none", files)
	}
}

func TestSessionInputResourceRejectsGenericFilestoreMutations(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-file-logical-view-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-file-logical-view-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-file-logical-view-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	file := uploadFile(t, app, "logical-view.txt", "text/plain", []byte("shared object"))
	defer deleteFile(t, app, file.ID)

	beforeStorageBytes := defaultWorkspaceStorageBytes(t, app)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+
		`,"environment_id":`+quoteJSON(env.ID)+
		`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+
		`,"mount_path":"/locked/input.txt"}]}`)
	defer deleteSession(t, app, session.ID)
	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(), record.WorkspaceID, record.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	for name, mutate := range map[string]func() error{
		"move input": func() error {
			_, err := app.db.MoveFilestoreFile(context.Background(), db.MoveFilestoreFileInput{
				WorkspaceID: record.WorkspaceID, FilesystemID: filesystem.ID,
				SourcePath: "/uploads/locked/input.txt", DestinationPath: "/uploads/locked/moved.txt",
			})
			return err
		},
		"move parent": func() error {
			_, err := app.db.MoveFilestoreDirectory(context.Background(), db.MoveFilestoreDirectoryInput{
				WorkspaceID: record.WorkspaceID, FilesystemID: filesystem.ID,
				SourcePath: "/uploads/locked", DestinationPath: "/uploads/moved",
			})
			return err
		},
		"remove input": func() error {
			_, err := app.db.RemoveFilestoreFile(context.Background(), db.RemoveSessionNamespaceNodeInput{
				WorkspaceID: record.WorkspaceID, FilesystemID: filesystem.ID,
				Path: "/uploads/locked/input.txt",
			})
			return err
		},
		"remove parent": func() error {
			_, err := app.db.RemoveFilestoreDirectory(context.Background(), db.RemoveFilestoreDirectoryInput{
				WorkspaceID: record.WorkspaceID, FilesystemID: filesystem.ID,
				Path: "/uploads/locked", Recursive: true,
			})
			return err
		},
		"overwrite input": func() error {
			_, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
				WorkspaceID: record.WorkspaceID, FilesystemID: filesystem.ID,
				Path: "/uploads/locked/input.txt", Blob: workspaceStorageBlob(9, nil),
				OverwriteExisting: true,
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); !errors.Is(err, db.ErrPreconditionFailed) {
				t.Fatalf("mutation error = %v, want ErrPreconditionFailed", err)
			}
		})
	}
	if got := defaultWorkspaceStorageBytes(t, app); got != beforeStorageBytes {
		t.Fatalf("storage after rejected mutations = %d, want %d", got, beforeStorageBytes)
	}
	if _, err := app.db.GetFile(context.Background(), record.WorkspaceID, file.ID); err != nil {
		t.Fatalf("rejected mutations changed source File: %v", err)
	}
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
	resources, err := app.db.ListSessionResources(context.Background(), sessionRecord.WorkspaceID, session.ID)
	if err != nil {
		t.Fatalf("list resources after concurrent mutation: %v", err)
	}

	switch {
	case bind.status == http.StatusOK && deleted.status == http.StatusConflict:
		if len(resources) != 1 {
			t.Fatalf("successful bind persisted resources = %d, want 1", len(resources))
		}
		if _, err := app.db.GetFile(context.Background(), sessionRecord.WorkspaceID, file.ID); err != nil {
			t.Fatalf("source file missing after bind won race: %v", err)
		}
	case bind.status == http.StatusNotFound && deleted.status == http.StatusOK:
		if len(resources) != 0 {
			t.Fatalf("rejected bind persisted resources = %d, want 0", len(resources))
		}
		if _, err := app.db.GetFile(context.Background(), sessionRecord.WorkspaceID, file.ID); !errors.Is(err, db.ErrNotFound) {
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
		sessionRecord.WorkspaceID,
		session.ID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	fileRecord, err := app.db.GetFile(
		context.Background(),
		sessionRecord.WorkspaceID,
		file.ID,
	)
	if err != nil {
		t.Fatalf("load source File: %v", err)
	}
	var filesBytesBefore, filestoreBytesBefore int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		select files_bytes, filestore_bytes
		from workspace_storage_usage
		where workspace_id = $1
	`, sessionRecord.WorkspaceID).Scan(&filesBytesBefore, &filestoreBytesBefore); err != nil {
		t.Fatalf("load storage usage before Session retirement: %v", err)
	}

	deleteSession(t, app, session.ID)
	sessionDeleted = true
	if _, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		sessionRecord.WorkspaceID,
		session.ID,
	); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("retired Session filesystem lookup error = %v, want ErrNotFound", err)
	}

	var cleanupJobID int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		update jobs
		set status = 'running',
			locked_by = 'input-resource-reference-retirement-test',
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
		returning id
	`, filesystem.UUID).Scan(&cleanupJobID); err != nil {
		t.Fatalf("lease Session filesystem cleanup: %v", err)
	}
	done, _, err := app.db.ProcessLeasedFilestoreFilesystemCleanupJob(
		context.Background(),
		cleanupJobID,
		"input-resource-reference-retirement-test",
		100,
	)
	if err != nil || !done {
		t.Fatalf("process Session filesystem cleanup = done %v, error %v", done, err)
	}

	if _, exists := store.objects[fileRecord.S3Key]; !exists {
		t.Fatal("Session filesystem cleanup deleted the Input Resource source object")
	}
	var activeEntries, filestoreObjectJobs int
	var filesBytesAfter, filestoreBytesAfter int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		select
			(select count(*)
			 from session_resources
			 where session_id = $1 and deleted_at is null),
			(select count(*)
			 from jobs
			 where type = 'filestore_object_cleanup'
				and payload->>'filesystem_uuid' = $2
				and payload->>'reason' = 'session_deleted'),
			coalesce(files_bytes, 0),
			coalesce(filestore_bytes, 0)
		from workspace_storage_usage
		where workspace_id = $3
	`, filesystem.SessionID, filesystem.UUID, sessionRecord.WorkspaceID).Scan(
		&activeEntries,
		&filestoreObjectJobs,
		&filesBytesAfter,
		&filestoreBytesAfter,
	); err != nil {
		t.Fatalf("load input-resource-reference cleanup state: %v", err)
	}
	if activeEntries != 0 || filestoreObjectJobs != 0 {
		t.Fatalf(
			"input-resource-reference cleanup = active entries %d, object jobs %d; want 0, 0",
			activeEntries,
			filestoreObjectJobs,
		)
	}
	if filesBytesAfter != filesBytesBefore || filestoreBytesAfter != filestoreBytesBefore {
		t.Fatalf(
			"storage usage after input-resource-reference cleanup = files %d filestore %d, want files %d filestore %d",
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
	persisted, err := app.db.ListSessionResources(context.Background(), session.WorkspaceID, session.ExternalID)
	if err != nil {
		t.Fatalf("list resources after concurrent add: %v", err)
	}
	if len(persisted) != 100 {
		t.Fatalf("persisted resources = %d, want 100", len(persisted))
	}
}

func assertFileResourcePayload(t *testing.T, raw json.RawMessage, sourceFileID, source, mountPath string) {
	t.Helper()
	var payload struct {
		FileID    string `json:"file_id"`
		Source    string `json:"source"`
		MountPath string `json:"mount_path"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode file resource: %v", err)
	}
	if payload.FileID != sourceFileID || payload.Source != source || payload.MountPath != mountPath {
		t.Fatalf("file resource = %+v, want Source File %q, source=%q mount_path=%q", payload, sourceFileID, source, mountPath)
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
		ID     string `json:"id"`
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.ID == "" {
		t.Fatalf("decode file resource ID: payload=%s error=%v", raw, err)
	}
	session := mustSessionRecord(t, app, sessionExternalID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		session.WorkspaceID,
		session.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	entry, err := app.db.GetSessionNamespaceNode(
		context.Background(),
		session.WorkspaceID,
		filesystem.ID,
		entryPath,
	)
	if err != nil {
		t.Fatalf("load Session file reference %q: %v", entryPath, err)
	}
	file, err := app.db.GetFile(context.Background(), session.WorkspaceID, fileExternalID)
	if err != nil {
		t.Fatalf("load source File: %v", err)
	}
	resource, err := app.db.GetSessionResource(
		context.Background(),
		session.WorkspaceID,
		session.ExternalID,
		payload.ID,
	)
	if err != nil {
		t.Fatalf("load Session file resource: %v", err)
	}
	if entry.Kind != db.SessionNamespaceNodeKindFile ||
		entry.UUID != resource.UUID ||
		payload.FileID != fileExternalID ||
		entry.ReferencedFileUUID == nil ||
		*entry.ReferencedFileUUID != file.UUID ||
		entry.SkillVersionUUID != nil ||
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
	storageBytes, err := app.db.GetWorkspaceStorageBytes(context.Background(), apiKey.WorkspaceID)
	if err != nil {
		t.Fatalf("load default workspace storage usage: %v", err)
	}
	return storageBytes
}

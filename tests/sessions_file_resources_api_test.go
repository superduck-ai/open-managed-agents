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

	t.Run("success defaults and add resource use uploads", func(t *testing.T) {
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
		var expectedBytes int64
		if err := app.db.Pool.QueryRow(context.Background(), `
			select
				coalesce((
					select sum(size_bytes)
					from files
					where workspace_id = $1 and deleted_at is null
				), 0)
				+
				coalesce((
					select sum(size_bytes)
					from filestore_entries
					where workspace_uuid = (
						select uuid from workspaces where id = $1
					)
						and kind = 'file'
						and source_file_uuid is null
						and deleted_at is null
				), 0)
		`, sessionRecord.WorkspaceID).Scan(&expectedBytes); err != nil {
			t.Fatalf("calculate expected workspace storage usage: %v", err)
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
		if _, err := app.db.GetFilestoreEntry(
			context.Background(),
			session.WorkspaceID,
			filesystem.ID,
			"/uploads/workspace/data.csv",
		); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("deleted file resource entry error = %v, want ErrNotFound", err)
		}
		parent, err := app.db.GetFilestoreEntry(
			context.Background(),
			session.WorkspaceID,
			filesystem.ID,
			"/uploads/workspace",
		)
		if err != nil {
			t.Fatalf("resource delete pruned the database-maintained parent directory: %v", err)
		}
		if parent.Kind != db.FilestoreEntryKindDirectory {
			t.Fatalf("resource parent kind = %q, want directory", parent.Kind)
		}
		if _, err := app.db.GetFile(context.Background(), session.WorkspaceID, file.ID); err != nil {
			t.Fatalf("source File was changed by resource delete: %v", err)
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
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+
		`,"environment_id":`+quoteJSON(env.ID)+
		`,"resources":[{"type":"file","file_id":`+quoteJSON(file.ID)+
		`,"mount_path":"/workspace/protected.txt"}]}`)
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
	sessionRecord := mustSessionRecord(t, app, session.ID)
	fileRecord, err := app.db.GetFile(
		context.Background(),
		sessionRecord.WorkspaceID,
		file.ID,
	)
	if err != nil {
		t.Fatalf("load protected File: %v", err)
	}

	t.Run("failure borrowed entry cannot be copied as Filestore-owned data", func(t *testing.T) {
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
			DestinationS3Bucket: "borrowed-copy-must-not-commit",
			DestinationS3Key:    "borrowed-copy-must-not-commit",
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
		if _, err := app.db.GetFilestoreEntry(
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
		returning id
	`, filesystem.UUID).Scan(&cleanupJobID); err != nil {
		t.Fatalf("lease Session filesystem cleanup: %v", err)
	}
	done, err := app.db.ProcessLeasedFilestoreFilesystemCleanupJob(
		context.Background(),
		cleanupJobID,
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
		where workspace_id = $2
	`, filesystem.UUID, sessionRecord.WorkspaceID).Scan(
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
	persisted, err := app.db.ListSessionResources(context.Background(), session.WorkspaceID, session.ExternalID)
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
		session.WorkspaceID,
		session.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	entry, err := app.db.GetFilestoreEntry(
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
	var filestoreBytes int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		select coalesce(filestore_bytes, 0)
		from workspace_storage_usage
		where workspace_id = $1
	`, session.WorkspaceID).Scan(&filestoreBytes); err != nil {
		t.Fatalf("load Filestore storage usage: %v", err)
	}
	if filestoreBytes != 0 {
		t.Fatalf("Filestore storage bytes = %d, borrowed references must count as zero", filestoreBytes)
	}
	return payload.ID
}

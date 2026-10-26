//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"net/http"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionFilesExcludeAccountUploads(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-scope-source-bucket"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-scope-source-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-scope-source-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	source := uploadFile(t, app, "input.txt", "text/plain", []byte("source bytes"))
	defer deleteFile(t, app, source.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`,"resources":[{"type":"file","file_id":`+quoteJSON(source.ID)+`,"mount_path":"/input"}]}`)
	defer deleteSession(t, app, session.ID)
	scope := "scope_id=" + session.ID

	t.Run("failure account upload cannot anchor scoped pagination", func(t *testing.T) {
		for _, direction := range []string{"after_id", "before_id"} {
			page := listFiles(t, app, scope+"&"+direction+"="+source.ID)
			if len(page.Data) != 0 || page.HasMore || page.NextPage != nil {
				t.Fatalf("upload cursor exposed scoped files: %+v", page)
			}
		}
	})

	t.Run("success input alone does not scope source", func(t *testing.T) {
		page := listFiles(t, app, scope)
		if len(page.Data) != 1 || page.Data[0].ID == source.ID || page.HasMore || page.NextPage != nil {
			t.Fatalf("input-only session list = %+v, want new instance", page)
		}
		if all := listFiles(t, app, ""); !containsFile(all.Data, source.ID) {
			t.Fatal("source missing from account list")
		}
	})

	record := mustSessionRecord(t, app, session.ID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), record.WorkspaceUUID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var scopedFileIDs []string
	for _, path := range []string{"first.txt", "second.txt", "third.txt"} {
		if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceUUID: record.WorkspaceUUID, FilesystemUUID: filesystem.UUID,
			Path: "/outputs/" + path, Blob: workspaceStorageBlob(1, nil),
		}); err != nil {
			t.Fatal(err)
		}
	}
	files, err := app.db.ListFiles(context.Background(), record.WorkspaceUUID, session.ID)
	if err != nil || len(files) != 4 {
		t.Fatalf("unpaged scoped files = %+v, %v, want an input instance and three outputs", files, err)
	}
	for _, file := range files {
		if file.ExternalID == source.ID {
			t.Fatal("source leaked through unpaged query")
		}
		scopedFileIDs = append(scopedFileIDs, file.ExternalID)
	}

	t.Run("failure source cursor remains outside scope with outputs", func(t *testing.T) {
		page := listFiles(t, app, scope+"&after_id="+source.ID)
		if len(page.Data) != 0 || page.HasMore || page.NextPage != nil {
			t.Fatalf("source cursor selected outputs: %+v", page)
		}
	})

	t.Run("success formal SDK paginates outputs with input attached", func(t *testing.T) {
		client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
		pager := client.Files.ListAutoPaging(context.Background(), anthropic.FileListParams{Limit: anthropic.Int(1)}, option.WithQuery("scope_id", session.ID), option.WithHeader("anthropic-beta", "managed-agents-2026-04-01"))
		seen := make(map[string]bool)
		for pager.Next() {
			id := pager.Current().ID
			if id == source.ID || seen[id] || len(seen) >= len(scopedFileIDs) {
				t.Fatalf("unexpected scoped file %q", id)
			}
			seen[id] = true
		}
		if pager.Err() != nil || len(seen) != len(scopedFileIDs) {
			t.Fatalf("scoped pagination = %v, %v", seen, pager.Err())
		}
		for _, id := range scopedFileIDs {
			if !seen[id] {
				t.Fatalf("missing output %q", id)
			}
		}
	})

	metadata := app.do(t, http.MethodGet, "/v1/files/"+source.ID, nil, defaultTestKey, false, "")
	defer metadata.Body.Close()
	var got metadataResponse
	decodeJSON(t, metadata.Body, &got)
	if got.ID != source.ID || got.Filename != source.Filename || got.CreatedAt != source.CreatedAt || got.Downloadable != source.Downloadable {
		t.Fatalf("source metadata changed: %+v, want %+v", got, source)
	}
}

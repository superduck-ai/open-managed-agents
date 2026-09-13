package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

func TestFilestoreMemoryNamespaceContract(t *testing.T) {
	objects := newFakeStore("filestore-memory-namespace")
	app := newTestAppWithStore(t, nil, objects)
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"filestore-memory-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"filestore-memory-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)

	t.Run("M2-03 read_only writes are rejected", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "ro-store", `"access":"read_only"`)
		defer fx.cleanup()
		seed := createMemory(t, app, fx.store.ID, "/notes/seed.md", "seed")

		resp := fx.createFile(t, "/memory/"+fx.slug()+"/notes/seed.md", []byte("mutated"))
		assertFilestoreError(t, resp, http.StatusForbidden, "permission_denied")
		got := retrieveMemory(t, app, fx.store.ID, seed.ID, defaultTestKey)
		if got.ContentSHA256 != seed.ContentSHA256 {
			t.Fatalf("read_only write mutated head hash %s -> %s", seed.ContentSHA256, got.ContentSHA256)
		}
		if versionsHaveSessionActor(t, app, fx.store.ID, fx.session.ID) {
			t.Fatal("read_only write created a session_actor version")
		}
	})

	t.Run("M2-04 unmounted slug is not found", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "mounted-store", "")
		defer fx.cleanup()

		resp := fx.createFile(t, "/memory/not-attached/notes/a.txt", []byte("secret"))
		assertFilestoreError(t, resp, http.StatusNotFound, "not_found")
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt"); found {
			t.Fatal("unmounted slug write leaked into the attached store")
		}
	})

	t.Run("M2-05 parent directory is not a memory namespace", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "parent-store", "")
		defer fx.cleanup()

		for _, path := range []string{"/memory/MEMORY.md", "/memory/foo.md"} {
			resp := fx.createFile(t, path, []byte("local only"))
			defer resp.Body.Close()
			if _, found := findMemoryByPath(t, app, fx.store.ID, "/MEMORY.md"); found {
				t.Fatalf("%s created a memory at /MEMORY.md", path)
			}
			if _, found := findMemoryByPath(t, app, fx.store.ID, "/foo.md"); found {
				t.Fatalf("%s created a memory at /foo.md", path)
			}
			if versionsHaveSessionActor(t, app, fx.store.ID, fx.session.ID) {
				t.Fatalf("%s produced a session_actor version", path)
			}
		}
	})

	t.Run("M2-07 content and item limits", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "limit-store", "")
		defer fx.cleanup()

		resp := fx.createFile(t, "/memory/"+fx.slug()+"/notes/too-big.txt", bytes.Repeat([]byte("a"), 102401))
		if resp.StatusCode == http.StatusOK {
			t.Fatal("100KB+1 write succeeded")
		}
		resp.Body.Close()
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/too-big.txt"); found {
			t.Fatal("oversize write created a memory head")
		}

		seedMemories(t, app, fx, 2000)
		resp = fx.createFile(t, "/memory/"+fx.slug()+"/notes/too-many.txt", []byte("201st wait 2001"))
		if resp.StatusCode == http.StatusOK {
			t.Fatal("2001st create succeeded")
		}
		resp.Body.Close()
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/too-many.txt"); found {
			t.Fatal("2001st create created a memory head")
		}

		resp = fx.createFile(t, "/memory/"+fx.slug()+"/seed/1999.txt", []byte("overwrite last"))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("overwrite 2000th item status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		got, found := findMemoryByPath(t, app, fx.store.ID, "/seed/1999.txt")
		if !found {
			t.Fatal("overwrite of 2000th item missing")
		}
		full := retrieveMemoryFull(t, app, fx.store.ID, got.ID)
		if full.Content == nil || *full.Content != "overwrite last" {
			t.Fatalf("overwrite content = %#v", full.Content)
		}
	})

	t.Run("M2-11 cross-namespace copy and move are rejected", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "cross-store", "")
		defer fx.cleanup()
		createOK := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("keep"))
		defer createOK.Body.Close()
		if createOK.StatusCode != http.StatusOK {
			t.Fatalf("seed create status = %d: %s", createOK.StatusCode, readAll(t, createOK.Body))
		}

		copyResp := fx.json(t, "copyFile", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"source":       "/memory/" + fx.slug() + "/notes/a.txt",
			"destination":  "/outputs/leaked.txt",
		})
		if copyResp.StatusCode == http.StatusOK {
			t.Fatal("copy into /outputs succeeded")
		}
		copyResp.Body.Close()

		moveResp := fx.json(t, "moveFile", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"source":       "/memory/" + fx.slug() + "/notes/a.txt",
			"destination":  "/outputs/moved.txt",
		})
		if moveResp.StatusCode == http.StatusOK {
			t.Fatal("move into /outputs succeeded")
		}
		moveResp.Body.Close()
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt"); !found {
			t.Fatal("cross-namespace move removed the memory")
		}
	})

	t.Run("M2-12 archived store rejects mutation and deleted store is fail-closed", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "lifecycle-store", "")
		defer fx.cleanup()
		createOK := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("before archive"))
		defer createOK.Body.Close()
		if createOK.StatusCode != http.StatusOK {
			t.Fatalf("seed create status = %d: %s", createOK.StatusCode, readAll(t, createOK.Body))
		}
		head, _ := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt")

		archiveMemoryStore(t, app, fx.store.ID)
		resp := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("after archive"))
		if resp.StatusCode == http.StatusOK {
			t.Fatal("write to archived store succeeded")
		}
		resp.Body.Close()
		got := retrieveMemory(t, app, fx.store.ID, head.ID, defaultTestKey)
		if got.ContentSHA256 != head.ContentSHA256 {
			t.Fatal("archived store write mutated the head")
		}

		deleteMemoryStore(t, app, fx.store.ID)
		readResp := fx.json(t, "readFile", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"path":         "/memory/" + fx.slug() + "/notes/a.txt",
		})
		assertFilestoreError(t, readResp, http.StatusNotFound, "not_found")
	})

	t.Run("M2-14 REST content_sha256 409 still holds beside filestore LWW", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "precondition-store", "")
		defer fx.cleanup()
		seed := createMemory(t, app, fx.store.ID, "/notes/a.txt", "rest-seed")
		writeResp := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("from-filestore"))
		defer writeResp.Body.Close()
		if writeResp.StatusCode != http.StatusOK {
			t.Fatalf("filestore LWW status = %d: %s", writeResp.StatusCode, readAll(t, writeResp.Body))
		}
		wrongHash := strings.Repeat("0", 64)
		resp := doMemoryRequest(
			t, app, http.MethodPost,
			"/v1/memory_stores/"+fx.store.ID+"/memories/"+seed.ID+"?beta=true",
			strings.NewReader(`{"content":"stale","precondition":{"type":"content_sha256","content_sha256":"`+wrongHash+`"}}`),
			defaultTestKey, true,
		)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("REST stale sha status = %d, want 409: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("M2-01 rw write becomes a session_actor version", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "rw-store", "")
		defer fx.cleanup()

		resp := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("hello-memory"))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("createFile status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		got, found := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt")
		if !found {
			t.Fatal("memory API did not observe /notes/a.txt")
		}
		full := retrieveMemoryFull(t, app, fx.store.ID, got.ID)
		if full.Content == nil || *full.Content != "hello-memory" {
			t.Fatalf("memory content = %#v", full.Content)
		}
		if !versionsHaveSessionActor(t, app, fx.store.ID, fx.session.ID) {
			t.Fatal("missing session_actor version for the writing session")
		}
		record, err := app.db.GetMemory(context.Background(), fx.workspaceUUID, fx.store.ID, got.ID)
		if err != nil {
			t.Fatalf("GetMemory: %v", err)
		}
		storeRecord, err := app.db.GetMemoryStore(context.Background(), fx.workspaceUUID, fx.store.ID)
		if err != nil {
			t.Fatalf("GetMemoryStore: %v", err)
		}
		prefix := "workspaces/" + fx.workspaceUUID + "/memory_stores/" + storeRecord.UUID + "/memories/"
		if !strings.HasPrefix(record.S3Key, prefix) || !strings.Contains(record.S3Key, "/versions/") || !strings.HasSuffix(record.S3Key, "/content") {
			t.Fatalf("S3 key = %q, want %s.../versions/.../content", record.S3Key, prefix)
		}
		object, ok := objects.objects[record.S3Key]
		if !ok || string(object.data) != "hello-memory" {
			t.Fatalf("object store missing written body for %s", record.S3Key)
		}
	})

	t.Run("M2-02 seed written via REST is readable through filestore", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "seed-store", "")
		defer fx.cleanup()
		createMemory(t, app, fx.store.ID, "/notes/seed.md", "console-seed")

		resp := fx.json(t, "readFile", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"path":         "/memory/" + fx.slug() + "/notes/seed.md",
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("readFile status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		body := string(readAll(t, resp.Body))
		if body != "console-seed" {
			t.Fatalf("readFile body = %q", body)
		}
	})

	t.Run("M2-06 last writer wins on the same path", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "lww-store", "")
		defer fx.cleanup()
		first := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("first"))
		defer first.Body.Close()
		second := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("second"))
		defer second.Body.Close()
		if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK {
			t.Fatalf("LWW writes status = %d, %d", first.StatusCode, second.StatusCode)
		}
		got, _ := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt")
		full := retrieveMemoryFull(t, app, fx.store.ID, got.ID)
		if full.Content == nil || *full.Content != "second" {
			t.Fatalf("LWW head = %#v", full.Content)
		}
		versions := listMemoryVersions(t, app, fx.store.ID, "memory_id="+got.ID+"&limit=20")
		if len(versions.Data) < 2 {
			t.Fatalf("want at least two versions, got %d", len(versions.Data))
		}
	})

	t.Run("M2-08 removeFile soft-deletes and keeps history", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "delete-store", "")
		defer fx.cleanup()
		createOK := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("gone soon"))
		defer createOK.Body.Close()
		head, _ := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt")

		resp := fx.json(t, "removeFile", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"path":         "/memory/" + fx.slug() + "/notes/a.txt",
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("removeFile status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt"); found {
			t.Fatal("deleted path still in memory head")
		}
		versions := listMemoryVersions(t, app, fx.store.ID, "memory_id="+head.ID+"&limit=20")
		sawDeleted := false
		for _, version := range versions.Data {
			if version.Operation == "deleted" {
				sawDeleted = true
				loaded := retrieveMemoryVersion(t, app, fx.store.ID, version.ID)
				if loaded.ID != version.ID {
					t.Fatalf("deleted version unreadable: %+v", loaded)
				}
			}
		}
		if !sawDeleted {
			t.Fatalf("missing deleted version: %+v", versions.Data)
		}
	})

	t.Run("createFile rejects ttlSeconds on memory files", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "ttl-store", "")
		defer fx.cleanup()

		resp := fx.createFileWithParams(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("temp"), map[string]any{
			"ttlSeconds": "60",
		})
		assertFilestoreError(t, resp, http.StatusBadRequest, "invalid_argument")
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt"); found {
			t.Fatal("ttlSeconds write created a permanent memory")
		}
	})

	t.Run("identical createFile does not leave an unreferenced object", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "noop-store", "")
		defer fx.cleanup()

		first := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("same-bytes"))
		defer first.Body.Close()
		if first.StatusCode != http.StatusOK {
			t.Fatalf("first createFile status = %d: %s", first.StatusCode, readAll(t, first.Body))
		}
		afterFirst := fakeStoreKeys(objects)

		second := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("same-bytes"))
		defer second.Body.Close()
		if second.StatusCode != http.StatusOK {
			t.Fatalf("identical createFile status = %d: %s", second.StatusCode, readAll(t, second.Body))
		}
		afterSecond := fakeStoreKeys(objects)
		if !sameStringSet(afterFirst, afterSecond) {
			t.Fatalf("identical rewrite leaked objects: before=%v after=%v", afterFirst, afterSecond)
		}
	})

	t.Run("M2-09 moveFile inside the same store", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "rename-store", "")
		defer fx.cleanup()
		createOK := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("renamed"))
		defer createOK.Body.Close()

		resp := fx.json(t, "moveFile", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"source":       "/memory/" + fx.slug() + "/notes/a.txt",
			"destination":  "/memory/" + fx.slug() + "/notes/b.txt",
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("moveFile status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt"); found {
			t.Fatal("old path still present after move")
		}
		got, found := findMemoryByPath(t, app, fx.store.ID, "/notes/b.txt")
		if !found {
			t.Fatal("new path missing after move")
		}
		full := retrieveMemoryFull(t, app, fx.store.ID, got.ID)
		if full.Content == nil || *full.Content != "renamed" {
			t.Fatalf("moved content = %#v", full.Content)
		}
		if !memoryHasOperation(t, app, fx.store.ID, got.ID, "modified") {
			t.Fatal("rename did not write a modified version")
		}
	})

	t.Run("moveFile overwrites an existing destination", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "overwrite-move-store", "")
		defer fx.cleanup()
		sourceOK := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("kept"))
		defer sourceOK.Body.Close()
		destOK := fx.createFile(t, "/memory/"+fx.slug()+"/notes/b.txt", []byte("replaced"))
		defer destOK.Body.Close()

		resp := fx.json(t, "moveFile", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"source":       "/memory/" + fx.slug() + "/notes/a.txt",
			"destination":  "/memory/" + fx.slug() + "/notes/b.txt",
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("overwrite moveFile status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt"); found {
			t.Fatal("source path still present after overwrite move")
		}
		got, found := findMemoryByPath(t, app, fx.store.ID, "/notes/b.txt")
		if !found {
			t.Fatal("destination missing after overwrite move")
		}
		full := retrieveMemoryFull(t, app, fx.store.ID, got.ID)
		if full.Content == nil || *full.Content != "kept" {
			t.Fatalf("overwrite-move content = %#v", full.Content)
		}
	})

	t.Run("M2-10 mkdir is virtual and rmdir follows emptiness", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "dir-store", "")
		defer fx.cleanup()

		mkdir := fx.json(t, "makeDirectory", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"path":         "/memory/" + fx.slug() + "/notes",
			"makeParents":  true,
		})
		defer mkdir.Body.Close()
		if mkdir.StatusCode != http.StatusOK {
			t.Fatalf("makeDirectory status = %d: %s", mkdir.StatusCode, readAll(t, mkdir.Body))
		}
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes"); found {
			t.Fatal("mkdir persisted a memory document")
		}

		empty := fx.json(t, "removeDirectory", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"path":         "/memory/" + fx.slug() + "/notes",
		})
		defer empty.Body.Close()
		if empty.StatusCode != http.StatusOK {
			t.Fatalf("empty rmdir status = %d: %s", empty.StatusCode, readAll(t, empty.Body))
		}

		createOK := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("child"))
		defer createOK.Body.Close()
		nonEmpty := fx.json(t, "removeDirectory", map[string]any{
			"filesystemId": fx.filesystem.ExternalID,
			"path":         "/memory/" + fx.slug() + "/notes",
		})
		if nonEmpty.StatusCode == http.StatusOK {
			t.Fatal("non-empty rmdir succeeded")
		}
		nonEmpty.Body.Close()
		if _, found := findMemoryByPath(t, app, fx.store.ID, "/notes/a.txt"); !found {
			t.Fatal("non-empty rmdir deleted child memories")
		}
	})

	t.Run("M2-13 filestore JWT has no slug or store claims", func(t *testing.T) {
		fx := newMemoryFilestoreFixture(t, app, agent.ID, env.ID, "jwt-store", "")
		defer fx.cleanup()
		payload := decodeJWTPayload(t, fx.token)
		for _, key := range []string{"slug", "access", "memory_store_id", "session_id"} {
			if _, exists := payload[key]; exists {
				t.Fatalf("token contains %s: %#v", key, payload)
			}
		}
		resp := fx.createFile(t, "/memory/"+fx.slug()+"/notes/a.txt", []byte("authorized-by-db"))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("write with slug-free token status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})
}

type memoryFilestoreFixture struct {
	t             *testing.T
	app           *testApp
	store         memoryStoreAPIResponse
	session       sessionAPIResponse
	resource      memoryResourceAPIResponse
	filesystem    db.FilestoreFilesystem
	token         string
	workspaceUUID string
}

func newMemoryFilestoreFixture(t *testing.T, app *testApp, agentID, envID, storeName, extra string) memoryFilestoreFixture {
	t.Helper()
	store := createMemoryStore(t, app, storeName)
	session := createSession(t, app, sessionBodyWithMemoryResource(agentID, envID, store.ID, extra))
	ids := getDefaultDBIDs(t, app.pool)
	ctx := context.Background()
	filesystem, err := app.db.GetFilestoreFilesystemBySession(ctx, ids.WorkspaceUUID, session.ID)
	if err != nil {
		t.Fatalf("GetFilestoreFilesystemBySession: %v", err)
	}
	scope, err := app.db.GetFilestoreTokenScopeForSessionIssue(ctx, ids.WorkspaceUUID, session.ID)
	if err != nil {
		t.Fatalf("GetFilestoreTokenScopeForSessionIssue: %v", err)
	}
	token, err := app.filestoreCredentials.Issue(filestore.TokenIdentity{
		Subject:                   scope.AccountExternalID,
		OrgUUID:                   scope.OrganizationUUID,
		AccountUUID:               scope.AccountUUID,
		WorkspaceUUID:             scope.WorkspaceUUID,
		WorkspaceTaggedID:         scope.WorkspaceExternalID,
		ResolvedWorkspaceTaggedID: scope.WorkspaceExternalID,
		FilesystemID:              scope.FilesystemExternalID,
		OrgTaints:                 append([]string(nil), scope.OrgTaints...),
		WorkspaceCMEKEnabled:      scope.WorkspaceCMEKEnabled,
	})
	if err != nil {
		t.Fatalf("issue filestore token: %v", err)
	}
	return memoryFilestoreFixture{
		t:             t,
		app:           app,
		store:         store,
		session:       session,
		resource:      decodeMemoryResource(t, session.Resources[0]),
		filesystem:    filesystem,
		token:         token,
		workspaceUUID: ids.WorkspaceUUID,
	}
}

func (fx memoryFilestoreFixture) cleanup() {
	deleteSession(fx.t, fx.app, fx.session.ID)
	deleteMemoryStore(fx.t, fx.app, fx.store.ID)
}

func (fx memoryFilestoreFixture) slug() string {
	return strings.TrimPrefix(fx.resource.MountPath, "/mnt/memory/")
}

func (fx memoryFilestoreFixture) createFile(t *testing.T, path string, content []byte) *http.Response {
	t.Helper()
	return fx.createFileWithParams(t, path, content, nil)
}

func (fx memoryFilestoreFixture) createFileWithParams(t *testing.T, path string, content []byte, extra map[string]any) *http.Response {
	t.Helper()
	params := map[string]any{
		"filesystemId":      fx.filesystem.ExternalID,
		"path":              path,
		"mediaType":         "text/plain",
		"overwriteExisting": true,
	}
	for key, value := range extra {
		params[key] = value
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal createFile params: %v", err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	paramsPart, err := writer.CreateFormField("params")
	if err != nil {
		t.Fatalf("create params part: %v", err)
	}
	if _, err := paramsPart.Write(encoded); err != nil {
		t.Fatalf("write params: %v", err)
	}
	filePart, err := writer.CreateFormFile("file", "content")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := filePart.Write(content); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, fx.app.server.URL+"/v1/filestore/fs/createFile", &body)
	if err != nil {
		t.Fatalf("new createFile request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+fx.token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := fx.app.server.Client().Do(req)
	if err != nil {
		t.Fatalf("createFile: %v", err)
	}
	return resp
}

func (fx memoryFilestoreFixture) json(t *testing.T, op string, payload map[string]any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", op, err)
	}
	req, err := http.NewRequest(http.MethodPost, fx.app.server.URL+"/v1/filestore/fs/"+op, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new %s request: %v", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+fx.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := fx.app.server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return resp
}

func assertFilestoreError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, status, readAll(t, resp.Body))
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decodeJSON(t, resp.Body, &body)
	if body.Code != code {
		t.Fatalf("error code = %q, want %q (%s)", body.Code, code, body.Message)
	}
}

func findMemoryByPath(t *testing.T, app *testApp, storeID, path string) (memoryAPIResponse, bool) {
	t.Helper()
	ids := getDefaultDBIDs(t, app.pool)
	record, found, err := app.db.GetMemoryByPath(context.Background(), ids.WorkspaceUUID, storeID, path)
	if err != nil {
		t.Fatalf("GetMemoryByPath: %v", err)
	}
	if !found {
		return memoryAPIResponse{}, false
	}
	return memoryAPIResponse{
		ID:               record.ExternalID,
		ContentSHA256:    record.ContentSHA256,
		ContentSizeBytes: record.ContentSizeBytes,
		Path:             record.Path,
	}, true
}

func retrieveMemoryFull(t *testing.T, app *testApp, storeID, memoryID string) memoryAPIResponse {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodGet, "/v1/memory_stores/"+storeID+"/memories/"+memoryID+"?beta=true&view=full", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve memory full status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var memory memoryAPIResponse
	decodeJSON(t, resp.Body, &memory)
	return memory
}

func versionsHaveSessionActor(t *testing.T, app *testApp, storeID, sessionID string) bool {
	t.Helper()
	page := listMemoryVersions(t, app, storeID, "limit=100")
	for _, version := range page.Data {
		if version.CreatedBy.Type == "session_actor" && version.CreatedBy.SessionID == sessionID {
			return true
		}
	}
	return false
}

func memoryHasOperation(t *testing.T, app *testApp, storeID, memoryID, operation string) bool {
	t.Helper()
	page := listMemoryVersions(t, app, storeID, "memory_id="+memoryID+"&limit=20")
	for _, version := range page.Data {
		if version.Operation == operation {
			return true
		}
	}
	return false
}

func fakeStoreKeys(store *fakeStore) map[string]struct{} {
	keys := make(map[string]struct{}, len(store.objects))
	for key := range store.objects {
		keys[key] = struct{}{}
	}
	return keys
}

func sameStringSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, ok := right[key]; !ok {
			return false
		}
	}
	return true
}

func seedMemories(t *testing.T, app *testApp, fx memoryFilestoreFixture, count int) {
	t.Helper()
	ctx := context.Background()
	store, err := app.db.GetMemoryStore(ctx, fx.workspaceUUID, fx.store.ID)
	if err != nil {
		t.Fatalf("GetMemoryStore: %v", err)
	}
	now := time.Now().UTC()
	for i := range count {
		memoryID, err := ids.New("mem_")
		if err != nil {
			t.Fatalf("memory id: %v", err)
		}
		versionID, err := ids.New("memver_")
		if err != nil {
			t.Fatalf("version id: %v", err)
		}
		body := []byte("seed")
		sum := sha256.Sum256(body)
		_, err = app.db.CreateMemory(ctx, db.Memory{
			UUID:                  uuid.NewV4().String(),
			ExternalID:            memoryID,
			WorkspaceUUID:         fx.workspaceUUID,
			MemoryStoreExternalID: store.ExternalID,
			Path:                  fmt.Sprintf("/seed/%d.txt", i),
			ContentSizeBytes:      int64(len(body)),
			ContentSHA256:         fmt.Sprintf("%x", sum),
			S3Bucket:              "filestore-memory-namespace",
			S3Key:                 fmt.Sprintf("seed/%d", i),
			CreatedAt:             now,
		}, db.MemoryVersion{
			UUID:       uuid.NewV4().String(),
			ExternalID: versionID,
			Operation:  "created",
			CreatedBy:  db.MemoryActor{Type: "api_actor"},
			CreatedAt:  now,
		})
		if err != nil {
			t.Fatalf("seed memory %d: %v", i, err)
		}
	}
}

func decodeJWTPayload(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode jwt payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal jwt payload: %v", err)
	}
	return claims
}

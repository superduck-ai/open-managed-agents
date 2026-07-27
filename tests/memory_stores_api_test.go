package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type memoryStoreAPIResponse struct {
	ID          string            `json:"id"`
	CreatedAt   string            `json:"created_at"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	UpdatedAt   string            `json:"updated_at"`
	ArchivedAt  *string           `json:"archived_at"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata"`
}

type memoryStorePageAPIResponse struct {
	Data     []memoryStoreAPIResponse `json:"data"`
	NextPage *string                  `json:"next_page"`
}

type memoryAPIResponse struct {
	ID               string  `json:"id"`
	ContentSHA256    string  `json:"content_sha256"`
	ContentSizeBytes int64   `json:"content_size_bytes"`
	CreatedAt        string  `json:"created_at"`
	MemoryStoreID    string  `json:"memory_store_id"`
	MemoryVersionID  string  `json:"memory_version_id"`
	Path             string  `json:"path"`
	Type             string  `json:"type"`
	UpdatedAt        string  `json:"updated_at"`
	Content          *string `json:"content"`
}

type memoryPageAPIResponse struct {
	Data     []json.RawMessage `json:"data"`
	Prefixes []json.RawMessage `json:"prefixes"`
	NextPage *string           `json:"next_page"`
}

type memoryVersionAPIResponse struct {
	ID               string                  `json:"id"`
	CreatedAt        string                  `json:"created_at"`
	MemoryID         string                  `json:"memory_id"`
	MemoryStoreID    string                  `json:"memory_store_id"`
	Operation        string                  `json:"operation"`
	Type             string                  `json:"type"`
	Content          *string                 `json:"content"`
	ContentSHA256    *string                 `json:"content_sha256"`
	ContentSizeBytes *int64                  `json:"content_size_bytes"`
	CreatedBy        memoryActorAPIResponse  `json:"created_by"`
	Path             *string                 `json:"path"`
	RedactedAt       *string                 `json:"redacted_at"`
	RedactedBy       *memoryActorAPIResponse `json:"redacted_by"`
}

type memoryActorAPIResponse struct {
	Type      string `json:"type"`
	APIKeyID  string `json:"api_key_id"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
}

type memoryVersionPageAPIResponse struct {
	Data     []memoryVersionAPIResponse `json:"data"`
	NextPage *string                    `json:"next_page"`
}

func TestMemoryStoresAPI(t *testing.T) {
	store := newFakeStore("memory-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()

	t.Run("success missing beta header", func(t *testing.T) {
		resp := doMemoryRequest(t, app, http.MethodGet, "/v1/memory_stores?beta=true", nil, defaultTestKey, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("failure missing beta query", func(t *testing.T) {
		resp := doMemoryRequest(t, app, http.MethodGet, "/v1/memory_stores", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid store fields", func(t *testing.T) {
		resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores?beta=true", strings.NewReader(`{"name":""}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		resp = doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores?beta=true", strings.NewReader(`{"name":"bad metadata","metadata":{"count":1}}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure no foreign keys", func(t *testing.T) {
		var foreignKeyCount int
		if err := app.db.Pool.QueryRow(context.Background(), `
			select count(*)
			from pg_constraint con
			join pg_class cls on cls.oid = con.conrelid
			where con.contype = 'f'
				and cls.relname in ('memory_stores', 'memories', 'memory_versions')
		`).Scan(&foreignKeyCount); err != nil {
			t.Fatalf("count memory foreign keys: %v", err)
		}
		if foreignKeyCount != 0 {
			t.Fatalf("memory foreign key count = %d, want 0", foreignKeyCount)
		}
	})

	t.Run("success store memory version lifecycle", func(t *testing.T) {
		createdStore := createMemoryStore(t, app, "memory-api-store")
		defer deleteMemoryStore(t, app, createdStore.ID)
		if createdStore.Type != "memory_store" || createdStore.ArchivedAt != nil || createdStore.Metadata["scenario"] != "memory-api-store" {
			t.Fatalf("unexpected created store: %+v", createdStore)
		}

		retrievedStore := retrieveMemoryStore(t, app, createdStore.ID, defaultTestKey)
		if retrievedStore.ID != createdStore.ID || retrievedStore.Name != createdStore.Name {
			t.Fatalf("unexpected retrieved store: %+v", retrievedStore)
		}

		updatedStore := updateMemoryStore(t, app, createdStore.ID, `{"name":"memory-api-store-updated","description":null,"metadata":{"scenario":"updated","extra":"yes"}}`)
		if updatedStore.Name != "memory-api-store-updated" || updatedStore.Description != "" || updatedStore.Metadata["scenario"] != "updated" {
			t.Fatalf("unexpected updated store: %+v", updatedStore)
		}

		storePage := listMemoryStores(t, app, "limit=10")
		if !containsMemoryStore(storePage.Data, createdStore.ID) {
			t.Fatalf("store list missing %s: %+v", createdStore.ID, storePage.Data)
		}

		otherKey := "sk-ant-local-memory-other"
		seedWorkspaceKey(t, app.db, "org_memory_other_test", "workspace_memory_other_test", "api_key_memory_other_test", otherKey)
		resp := doMemoryRequest(t, app, http.MethodGet, "/v1/memory_stores/"+createdStore.ID+"?beta=true", nil, otherKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")

		resp = doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+createdStore.ID+"/memories?beta=true", strings.NewReader(`{"path":"xx","content":"invalid"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		firstContent := "hello memory"
		createdMemory := createMemory(t, app, createdStore.ID, "/projects/foo/notes.md", firstContent)
		assertMemoryContent(t, createdMemory, firstContent)
		firstVersionID := createdMemory.MemoryVersionID

		resp = doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+createdStore.ID+"/memories?beta=true", strings.NewReader(`{"path":"/projects/foo/notes.md","content":"duplicate"}`), defaultTestKey, true)
		assertMemoryPathConflict(t, resp, createdMemory.ID, "/projects/foo/notes.md")

		wrongHash := strings.Repeat("0", 64)
		resp = doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+createdStore.ID+"/memories/"+createdMemory.ID+"?beta=true", strings.NewReader(`{"content":"stale","precondition":{"type":"content_sha256","content_sha256":"`+wrongHash+`"}}`), defaultTestKey, true)
		assertError(t, resp, http.StatusConflict, "memory_precondition_failed_error")

		updatedContent := "updated memory"
		updatedMemory := updateMemory(t, app, createdStore.ID, createdMemory.ID, `{"path":"/projects/foo/renamed.md","content":"`+updatedContent+`","precondition":{"type":"content_sha256","content_sha256":"`+createdMemory.ContentSHA256+`"}}`)
		assertMemoryContent(t, updatedMemory, updatedContent)
		if updatedMemory.MemoryVersionID == firstVersionID || updatedMemory.Path != "/projects/foo/renamed.md" {
			t.Fatalf("unexpected updated memory: %+v", updatedMemory)
		}

		retrievedMemory := retrieveMemory(t, app, createdStore.ID, createdMemory.ID, defaultTestKey)
		assertMemoryContent(t, retrievedMemory, updatedContent)

		secondMemory := createMemory(t, app, createdStore.ID, "/projects/bar/todo.md", "todo")
		if secondMemory.MemoryStoreID != createdStore.ID {
			t.Fatalf("unexpected second memory: %+v", secondMemory)
		}

		memories := listMemories(t, app, createdStore.ID, "path_prefix="+url.QueryEscape("/projects/foo/")+"&limit=10")
		if !containsMemoryItem(t, memories.Data, createdMemory.ID) {
			t.Fatalf("memory list missing %s: %+v", createdMemory.ID, memories.Data)
		}

		depthPage := listMemories(t, app, createdStore.ID, "path_prefix="+url.QueryEscape("/projects/")+"&depth=1&limit=10")
		if !containsMemoryPrefix(t, depthPage.Data, "/projects/foo/") || !containsMemoryPrefix(t, depthPage.Data, "/projects/bar/") {
			t.Fatalf("depth page missing expected prefixes: %+v", depthPage.Data)
		}
		if !containsMemoryPrefix(t, depthPage.Prefixes, "/projects/foo/") || !containsMemoryPrefix(t, depthPage.Prefixes, "/projects/bar/") {
			t.Fatalf("depth page missing expected top-level prefixes: %+v", depthPage.Prefixes)
		}

		versions := listMemoryVersions(t, app, createdStore.ID, "memory_id="+url.QueryEscape(createdMemory.ID)+"&limit=10")
		if !containsMemoryVersionOperation(versions.Data, "created") || !containsMemoryVersionOperation(versions.Data, "modified") {
			t.Fatalf("version list missing created/modified: %+v", versions.Data)
		}

		firstVersion := retrieveMemoryVersion(t, app, createdStore.ID, firstVersionID)
		if firstVersion.Content == nil || *firstVersion.Content != firstContent || firstVersion.CreatedBy.Type != "api_actor" || firstVersion.CreatedBy.APIKeyID == "" {
			t.Fatalf("unexpected first version: %+v", firstVersion)
		}

		redacted := redactMemoryVersion(t, app, createdStore.ID, firstVersionID)
		if redacted.RedactedAt == nil || redacted.RedactedBy == nil || redacted.ContentSHA256 != nil || redacted.ContentSizeBytes != nil || redacted.Path != nil || redacted.Content != nil {
			t.Fatalf("unexpected redacted version: %+v", redacted)
		}
		redactedAgain := redactMemoryVersion(t, app, createdStore.ID, firstVersionID)
		if redactedAgain.RedactedAt == nil || redactedAgain.ContentSHA256 != nil || redactedAgain.Path != nil {
			t.Fatalf("unexpected idempotent redacted version: %+v", redactedAgain)
		}

		resp = doMemoryRequest(t, app, http.MethodDelete, "/v1/memory_stores/"+createdStore.ID+"/memories/"+createdMemory.ID+"?beta=true&expected_content_sha256="+wrongHash, nil, defaultTestKey, true)
		assertError(t, resp, http.StatusConflict, "memory_precondition_failed_error")

		deleteMemory(t, app, createdStore.ID, createdMemory.ID, updatedMemory.ContentSHA256)

		resp = doMemoryRequest(t, app, http.MethodGet, "/v1/memory_stores/"+createdStore.ID+"/memories/"+createdMemory.ID+"?beta=true", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")

		deletedVersions := listMemoryVersions(t, app, createdStore.ID, "memory_id="+url.QueryEscape(createdMemory.ID)+"&operation=deleted&limit=10")
		if len(deletedVersions.Data) != 1 || deletedVersions.Data[0].Path == nil || *deletedVersions.Data[0].Path != updatedMemory.Path {
			t.Fatalf("unexpected deleted versions: %+v", deletedVersions.Data)
		}

		deleteMemoryStore(t, app, createdStore.ID)
		if len(store.objects) != 0 {
			t.Fatalf("memory objects after store delete = %d, want 0", len(store.objects))
		}
	})

	t.Run("failure archived store rejects writes", func(t *testing.T) {
		archivedStore := createMemoryStore(t, app, "archived-memory-store")
		defer deleteMemoryStore(t, app, archivedStore.ID)

		archived := archiveMemoryStore(t, app, archivedStore.ID)
		if archived.ArchivedAt == nil {
			t.Fatalf("archived_at = nil, want timestamp")
		}

		resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+archivedStore.ID+"/memories?beta=true", strings.NewReader(`{"path":"/archived.txt","content":"nope"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})
}

func TestMemoryStoreObjectCleanupJobs(t *testing.T) {
	store := newFakeStore("memory-cleanup-bucket")
	store.deleteErr = errors.New("object storage unavailable")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()

	createdStore := createMemoryStore(t, app, "memory-cleanup-store")
	createdMemory := createMemory(t, app, createdStore.ID, "/cleanup.md", "cleanup later")
	var objectKey string
	for key := range store.objects {
		objectKey = key
		break
	}
	if objectKey == "" {
		t.Fatalf("expected memory object to be stored")
	}
	defer app.db.Pool.Exec(context.Background(), `delete from jobs where payload->>'key' = $1`, objectKey)

	deleteMemoryStore(t, app, createdStore.ID)

	var jobCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from jobs
		where type = 'object_cleanup'
			and status = 'pending'
			and payload->>'key' = $1
			and payload->>'resource_type' = 'memory_version'
			and payload->>'resource_id' = $2
	`, objectKey, createdMemory.MemoryVersionID).Scan(&jobCount); err != nil {
		t.Fatalf("count cleanup jobs: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("cleanup job count = %d, want 1", jobCount)
	}
}

func doMemoryRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new memory request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if betaHeader {
		req.Header.Set("anthropic-beta", "files-api-2025-04-14, managed-agents-2026-04-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do memory request: %v", err)
	}
	return resp
}

func createMemoryStore(t *testing.T, app *testApp, name string) memoryStoreAPIResponse {
	t.Helper()
	body := `{"name":` + quoteJSON(name) + `,"description":"test memory store","metadata":{"scenario":` + quoteJSON(name) + `}}`
	resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create memory store status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var store memoryStoreAPIResponse
	decodeJSON(t, resp.Body, &store)
	return store
}

func retrieveMemoryStore(t *testing.T, app *testApp, storeID, key string) memoryStoreAPIResponse {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodGet, "/v1/memory_stores/"+storeID+"?beta=true", nil, key, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve memory store status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var store memoryStoreAPIResponse
	decodeJSON(t, resp.Body, &store)
	return store
}

func updateMemoryStore(t *testing.T, app *testApp, storeID, body string) memoryStoreAPIResponse {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+storeID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update memory store status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var store memoryStoreAPIResponse
	decodeJSON(t, resp.Body, &store)
	return store
}

func archiveMemoryStore(t *testing.T, app *testApp, storeID string) memoryStoreAPIResponse {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+storeID+"/archive?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive memory store status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var store memoryStoreAPIResponse
	decodeJSON(t, resp.Body, &store)
	return store
}

func deleteMemoryStore(t *testing.T, app *testApp, storeID string) {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodDelete, "/v1/memory_stores/"+storeID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete memory store status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func listMemoryStores(t *testing.T, app *testApp, query string) memoryStorePageAPIResponse {
	t.Helper()
	path := "/v1/memory_stores?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doMemoryRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list memory stores status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page memoryStorePageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func createMemory(t *testing.T, app *testApp, storeID, path, content string) memoryAPIResponse {
	t.Helper()
	body := `{"path":` + quoteJSON(path) + `,"content":` + quoteJSON(content) + `}`
	resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+storeID+"/memories?beta=true&view=full", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create memory status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var memory memoryAPIResponse
	decodeJSON(t, resp.Body, &memory)
	return memory
}

func retrieveMemory(t *testing.T, app *testApp, storeID, memoryID, key string) memoryAPIResponse {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodGet, "/v1/memory_stores/"+storeID+"/memories/"+memoryID+"?beta=true", nil, key, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve memory status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var memory memoryAPIResponse
	decodeJSON(t, resp.Body, &memory)
	return memory
}

func updateMemory(t *testing.T, app *testApp, storeID, memoryID, body string) memoryAPIResponse {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+storeID+"/memories/"+memoryID+"?beta=true&view=full", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update memory status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var memory memoryAPIResponse
	decodeJSON(t, resp.Body, &memory)
	return memory
}

func deleteMemory(t *testing.T, app *testApp, storeID, memoryID, expectedSHA string) {
	t.Helper()
	path := "/v1/memory_stores/" + storeID + "/memories/" + memoryID + "?beta=true"
	if expectedSHA != "" {
		path += "&expected_content_sha256=" + url.QueryEscape(expectedSHA)
	}
	resp := doMemoryRequest(t, app, http.MethodDelete, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete memory status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func listMemories(t *testing.T, app *testApp, storeID, query string) memoryPageAPIResponse {
	t.Helper()
	path := "/v1/memory_stores/" + storeID + "/memories?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doMemoryRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list memories status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page memoryPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func listMemoryVersions(t *testing.T, app *testApp, storeID, query string) memoryVersionPageAPIResponse {
	t.Helper()
	path := "/v1/memory_stores/" + storeID + "/memory_versions?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doMemoryRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list memory versions status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page memoryVersionPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func retrieveMemoryVersion(t *testing.T, app *testApp, storeID, versionID string) memoryVersionAPIResponse {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodGet, "/v1/memory_stores/"+storeID+"/memory_versions/"+versionID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve memory version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var version memoryVersionAPIResponse
	decodeJSON(t, resp.Body, &version)
	return version
}

func redactMemoryVersion(t *testing.T, app *testApp, storeID, versionID string) memoryVersionAPIResponse {
	t.Helper()
	resp := doMemoryRequest(t, app, http.MethodPost, "/v1/memory_stores/"+storeID+"/memory_versions/"+versionID+"/redact?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redact memory version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var version memoryVersionAPIResponse
	decodeJSON(t, resp.Body, &version)
	return version
}

func assertMemoryContent(t *testing.T, memory memoryAPIResponse, content string) {
	t.Helper()
	if memory.Type != "memory" || memory.Content == nil || *memory.Content != content {
		t.Fatalf("unexpected memory content response: %+v, want content %q", memory, content)
	}
	if memory.ContentSHA256 != memorySHA256(content) || memory.ContentSizeBytes != int64(len([]byte(content))) {
		t.Fatalf("unexpected memory hash/size: %+v", memory)
	}
}

func assertMemoryPathConflict(t *testing.T, resp *http.Response, memoryID, path string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("path conflict status = %d, want 409: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type                string `json:"type"`
			ConflictingMemoryID string `json:"conflicting_memory_id"`
			ConflictingPath     string `json:"conflicting_path"`
		} `json:"error"`
	}
	decodeJSON(t, resp.Body, &body)
	if body.Type != "error" || body.Error.Type != "memory_path_conflict_error" || body.Error.ConflictingMemoryID != memoryID || body.Error.ConflictingPath != path {
		t.Fatalf("unexpected path conflict body: %+v", body)
	}
}

func containsMemoryStore(stores []memoryStoreAPIResponse, id string) bool {
	for _, store := range stores {
		if store.ID == id {
			return true
		}
	}
	return false
}

func containsMemoryItem(t *testing.T, items []json.RawMessage, id string) bool {
	t.Helper()
	for _, raw := range items {
		var item struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		decodeJSON(t, strings.NewReader(string(raw)), &item)
		if item.Type == "memory" && item.ID == id {
			return true
		}
	}
	return false
}

func containsMemoryPrefix(t *testing.T, items []json.RawMessage, path string) bool {
	t.Helper()
	for _, raw := range items {
		var item struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}
		decodeJSON(t, strings.NewReader(string(raw)), &item)
		if item.Type == "memory_prefix" && item.Path == path {
			return true
		}
	}
	return false
}

func containsMemoryVersionOperation(versions []memoryVersionAPIResponse, operation string) bool {
	for _, version := range versions {
		if version.Operation == operation {
			return true
		}
	}
	return false
}

func memorySHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

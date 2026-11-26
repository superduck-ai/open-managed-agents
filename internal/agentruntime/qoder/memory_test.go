package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
)

func TestQoderMemoryStoreLifecycleAndIDOR(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	var upstreamHits atomic.Int32
	var deletedRemote atomic.Bool
	var archivedRemote atomic.Bool
	var lastUpdateBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/memory_stores":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			meta := req["metadata"].(map[string]any)
			if meta[watermarkOrgKey] != "org-a" || meta[watermarkWorkspaceKey] != "ws-a" {
				t.Errorf("missing watermark metadata: %+v", meta)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "memstore_remote_1",
				"type":        "memory_store",
				"name":        req["name"],
				"description": req["description"],
				"status":      "active",
				"entry_count": 0,
				"total_size":  0,
				"metadata":    meta,
				"created_at":  now,
				"updated_at":  now,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/memory_stores/memstore_remote_1":
			status := "active"
			var archivedAt any
			if archivedRemote.Load() {
				status = "archived"
				archivedAt = now.Add(time.Minute)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "memstore_remote_1",
				"type":        "memory_store",
				"name":        "Notes Updated",
				"description": "Updated desc",
				"status":      status,
				"metadata":    map[string]string{watermarkOrgKey: "org-a", watermarkWorkspaceKey: "ws-a", "env": "prod"},
				"created_at":  now,
				"updated_at":  now.Add(time.Minute),
				"archived_at": archivedAt,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/memory_stores/memstore_remote_1":
			_ = json.NewDecoder(r.Body).Decode(&lastUpdateBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "memstore_remote_1",
				"type":        "memory_store",
				"name":        "Notes Updated",
				"description": "Updated desc",
				"status":      "active",
				"metadata":    map[string]string{watermarkOrgKey: "org-a", watermarkWorkspaceKey: "ws-a", "env": "prod"},
				"created_at":  now,
				"updated_at":  now.Add(time.Minute),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/memory_stores/memstore_remote_1/archive":
			archivedRemote.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "memstore_remote_1",
				"type":        "memory_store",
				"name":        "Notes Updated",
				"description": "Updated desc",
				"status":      "archived",
				"metadata":    map[string]string{watermarkOrgKey: "org-a", watermarkWorkspaceKey: "ws-a"},
				"created_at":  now,
				"updated_at":  now.Add(2 * time.Minute),
				"archived_at": now.Add(2 * time.Minute),
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/memory_stores/memstore_remote_1":
			deletedRemote.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "memstore_remote_1",
				"type": "memory_store_deleted",
			})
		default:
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()
	scopeA := agentruntime.Scope{
		OrganizationUUID:   "org-a",
		WorkspaceUUID:      "ws-a",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	scopeB := agentruntime.Scope{
		OrganizationUUID:   "org-b",
		WorkspaceUUID:      "ws-b",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}

	created, err := rt.CreateMemoryStore(ctx, scopeA, agentruntime.CreateMemoryStoreInput{
		Name:        "Notes",
		Description: "Project notes",
		Metadata:    map[string]string{"env": "dev"},
	})
	if err != nil {
		t.Fatalf("CreateMemoryStore: %v", err)
	}
	if !strings.HasPrefix(created.ID, "memstore_") || created.ID == "memstore_remote_1" {
		t.Fatalf("unexpected OMA memory store ID: %q", created.ID)
	}
	if created.Metadata["env"] != "dev" || created.Metadata[watermarkOrgKey] != "" {
		t.Fatalf("unexpected metadata: %+v", created.Metadata)
	}

	hitsBeforeIDOR := upstreamHits.Load()
	if _, err := rt.GetMemoryStore(ctx, scopeB, created.ID); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant GetMemoryStore err = %v, want ErrResourceNotFound", err)
	}
	if _, err := rt.GetMemoryStore(ctx, scopeA, "memstore_remote_1"); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("raw remote ID GetMemoryStore err = %v, want ErrResourceNotFound", err)
	}
	if _, err := rt.ListMemories(ctx, scopeB, created.ID, agentruntime.ListMemoriesQuery{}); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant ListMemories err = %v, want ErrResourceNotFound", err)
	}
	if _, err := rt.ListMemoryVersions(ctx, scopeB, created.ID, agentruntime.ListMemoryVersionsQuery{}); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant ListMemoryVersions err = %v, want ErrResourceNotFound", err)
	}
	if err := rt.DeleteMemoryStore(ctx, scopeB, created.ID); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant DeleteMemoryStore err = %v, want ErrResourceNotFound", err)
	}
	if upstreamHits.Load() != hitsBeforeIDOR {
		t.Fatalf("IDOR checks must not hit upstream: hits=%d want=%d", upstreamHits.Load(), hitsBeforeIDOR)
	}

	newName := "Notes Updated"
	newDesc := "Updated desc"
	updated, err := rt.UpdateMemoryStore(ctx, scopeA, created.ID, agentruntime.UpdateMemoryStoreInput{
		Name:        &newName,
		Description: &newDesc,
		Metadata:    map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("UpdateMemoryStore: %v", err)
	}
	if updated.ID != created.ID || updated.Name != "Notes Updated" || updated.Metadata["env"] != "prod" {
		t.Fatalf("unexpected updated store: %+v", updated)
	}
	if lastUpdateBody["name"] != "Notes Updated" {
		t.Fatalf("unexpected update body: %+v", lastUpdateBody)
	}

	pageA, err := rt.ListMemoryStores(ctx, scopeA, agentruntime.ListMemoryStoresQuery{Limit: 20})
	if err != nil {
		t.Fatalf("ListMemoryStores A: %v", err)
	}
	if len(pageA.Items) != 1 || pageA.Items[0].ID != created.ID {
		t.Fatalf("unexpected pageA: %+v", pageA)
	}
	pageB, err := rt.ListMemoryStores(ctx, scopeB, agentruntime.ListMemoryStoresQuery{Limit: 20})
	if err != nil {
		t.Fatalf("ListMemoryStores B: %v", err)
	}
	if len(pageB.Items) != 0 {
		t.Fatalf("tenant B should see 0 stores, got %d", len(pageB.Items))
	}

	archived, err := rt.ArchiveMemoryStore(ctx, scopeA, created.ID)
	if err != nil {
		t.Fatalf("ArchiveMemoryStore: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatalf("expected ArchivedAt to be set: %+v", archived)
	}

	activeOnly, err := rt.ListMemoryStores(ctx, scopeA, agentruntime.ListMemoryStoresQuery{IncludeArchived: false})
	if err != nil {
		t.Fatalf("ListMemoryStores activeOnly: %v", err)
	}
	if len(activeOnly.Items) != 0 {
		t.Fatalf("expected 0 active stores after archive, got %d", len(activeOnly.Items))
	}
	withArchived, err := rt.ListMemoryStores(ctx, scopeA, agentruntime.ListMemoryStoresQuery{IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListMemoryStores withArchived: %v", err)
	}
	if len(withArchived.Items) != 1 || withArchived.Items[0].ArchivedAt == nil {
		t.Fatalf("expected 1 archived store, got %+v", withArchived.Items)
	}

	if err := rt.DeleteMemoryStore(ctx, scopeA, created.ID); err != nil {
		t.Fatalf("DeleteMemoryStore: %v", err)
	}
	if !deletedRemote.Load() {
		t.Fatal("expected upstream DELETE /memory_stores/memstore_remote_1")
	}
	if _, err := rt.GetMemoryStore(ctx, scopeA, created.ID); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("GetMemoryStore after delete = %v, want ErrResourceNotFound", err)
	}
}

func TestQoderMemoriesPathNormalizationFullHydrateDepthAndVersions(t *testing.T) {
	now := time.Date(2026, 11, 27, 11, 0, 0, 0, time.UTC)
	var getMemoryHits atomic.Int32
	var getVersionHits atomic.Int32
	var lastCreatePath string
	var lastUpdatePath string
	var lastListPrefix string
	var lastVersionPathFilter string
	var lastDeleteExpectedHash string

	memoriesByID := map[string]map[string]any{
		"mem_1": {
			"id":                 "mem_1",
			"memory_store_id":    "memstore_remote_2",
			"memory_version_id":  "memver_1",
			"path":               "root.md",
			"content":            "root content",
			"content_size_bytes": 12,
			"content_sha256":     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"created_at":         now,
			"updated_at":         now,
		},
		"mem_2": {
			"id":                 "mem_2",
			"memory_store_id":    "memstore_remote_2",
			"version":            2,
			"path":               "notes/hello.md",
			"content":            "hello world",
			"content_size_bytes": 11,
			"content_sha256":     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"created_at":         now,
			"updated_at":         now,
		},
		"mem_3": {
			"id":                 "mem_3",
			"memory_store_id":    "memstore_remote_2",
			"memory_version_id":  "memver_3",
			"path":               "notes/sub/deep.md",
			"content":            "deep content",
			"content_size_bytes": 12,
			"content_sha256":     "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"created_at":         now,
			"updated_at":         now,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/memory_stores":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "memstore_remote_2", "type": "memory_store", "name": "KB", "status": "active", "created_at": now, "updated_at": now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/memory_stores/memstore_remote_2/memories":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			lastCreatePath, _ = req["path"].(string)
			if strings.HasPrefix(lastCreatePath, "/") {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "Field 'path' must be a relative path."})
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                 "mem_2",
				"memory_store_id":    "memstore_remote_2",
				"memory_version_id":  "memver_2",
				"path":               lastCreatePath,
				"content_size_bytes": len(req["content"].(string)),
				"content_sha256":     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"created_at":         now,
				"updated_at":         now,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/memory_stores/memstore_remote_2/memories":
			lastListPrefix = r.URL.Query().Get("path_prefix")
			if strings.HasPrefix(lastListPrefix, "/") {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "path_prefix must be relative"})
				return
			}
			list := make([]map[string]any, 0, 3)
			for _, id := range []string{"mem_1", "mem_2", "mem_3"} {
				item := memoriesByID[id]
				p := item["path"].(string)
				if lastListPrefix != "" && !strings.HasPrefix(p, lastListPrefix) {
					continue
				}
				list = append(list, map[string]any{
					"id":                 item["id"],
					"memory_store_id":    item["memory_store_id"],
					"memory_version_id":  item["memory_version_id"],
					"version":            item["version"],
					"path":               p,
					"content_size_bytes": item["content_size_bytes"],
					"content_sha256":     item["content_sha256"],
					"created_at":         item["created_at"],
					"updated_at":         item["updated_at"],
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":     list,
				"has_more": false,
				"first_id": "mem_1",
				"last_id":  "mem_3",
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/memory_stores/memstore_remote_2/memories/"):
			getMemoryHits.Add(1)
			memID := strings.TrimPrefix(r.URL.Path, "/memory_stores/memstore_remote_2/memories/")
			item, ok := memoriesByID[memID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(item)
		case r.Method == http.MethodPost && r.URL.Path == "/memory_stores/memstore_remote_2/memories/mem_2":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			lastUpdatePath, _ = req["path"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                 "mem_2",
				"memory_store_id":    "memstore_remote_2",
				"memory_version_id":  "memver_2b",
				"path":               lastUpdatePath,
				"content_size_bytes": 11,
				"content_sha256":     "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"created_at":         now,
				"updated_at":         now.Add(time.Minute),
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/memory_stores/memstore_remote_2/memories/mem_2":
			lastDeleteExpectedHash = r.URL.Query().Get("expected_content_sha256")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "mem_2", "type": "memory_deleted"})
		case r.Method == http.MethodGet && r.URL.Path == "/memory_stores/memstore_remote_2/memory_versions":
			lastVersionPathFilter = r.URL.Query().Get("path")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":                 "memver_1",
						"memory_id":          "mem_1",
						"memory_store_id":    "memstore_remote_2",
						"path":               "root.md",
						"content_size_bytes": 12,
						"content_sha256":     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"operation":          "created",
						"redacted":           false,
						"created_at":         now,
					},
					{
						"id":              "memver_redacted",
						"memory_id":       "mem_2",
						"memory_store_id": "memstore_remote_2",
						"operation":       "modified",
						"redacted":        true,
						"redacted_at":     now.Add(time.Minute),
						"created_at":      now,
					},
				},
				"has_more": false,
				"first_id": "memver_1",
				"last_id":  "memver_redacted",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/memory_stores/memstore_remote_2/memory_versions/memver_1":
			getVersionHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                 "memver_1",
				"memory_id":          "mem_1",
				"memory_store_id":    "memstore_remote_2",
				"path":               "root.md",
				"content":            "root content",
				"content_size_bytes": 12,
				"content_sha256":     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"operation":          "created",
				"redacted":           false,
				"created_at":         now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/memory_stores/memstore_remote_2/memory_versions/memver_1/redact":
			redactedAt := now.Add(2 * time.Minute)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              "memver_1",
				"memory_id":       "mem_1",
				"memory_store_id": "memstore_remote_2",
				"operation":       "created",
				"redacted":        true,
				"redacted_at":     redactedAt,
				"created_at":      now,
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()
	scope := agentruntime.Scope{
		OrganizationUUID:   "org-a",
		WorkspaceUUID:      "ws-a",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}

	store, err := rt.CreateMemoryStore(ctx, scope, agentruntime.CreateMemoryStoreInput{Name: "KB"})
	if err != nil {
		t.Fatalf("CreateMemoryStore: %v", err)
	}

	createdMem, err := rt.CreateMemory(ctx, scope, store.ID, agentruntime.CreateMemoryInput{
		Path:    "/notes/hello.md",
		Content: "hello world",
		View:    "full",
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	if lastCreatePath != "notes/hello.md" {
		t.Fatalf("outbound create path = %q, want notes/hello.md", lastCreatePath)
	}
	if createdMem.Path != "/notes/hello.md" || createdMem.MemoryStoreID != store.ID {
		t.Fatalf("created memory mismatch: %+v", createdMem)
	}
	if createdMem.Content == nil || *createdMem.Content != "hello world" {
		t.Fatalf("expected full content on CreateMemory: %+v", createdMem)
	}

	basicList, err := rt.ListMemories(ctx, scope, store.ID, agentruntime.ListMemoriesQuery{
		PathPrefix: "/notes/",
		View:       "basic",
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("ListMemories basic: %v", err)
	}
	if lastListPrefix != "notes/" {
		t.Fatalf("outbound path_prefix = %q, want notes/", lastListPrefix)
	}
	if getMemoryHits.Load() != 0 {
		t.Fatalf("view=basic must not hydrate individual memories, hits=%d", getMemoryHits.Load())
	}
	if len(basicList.Items) != 2 || basicList.Items[0].Path != "/notes/hello.md" || basicList.Items[0].Content != nil {
		t.Fatalf("unexpected basicList: %+v", basicList.Items)
	}
	if basicList.Items[0].MemoryVersionID != "2" {
		t.Fatalf("expected flex version fallback to \"2\", got %q", basicList.Items[0].MemoryVersionID)
	}

	fullList, err := rt.ListMemories(ctx, scope, store.ID, agentruntime.ListMemoriesQuery{
		PathPrefix: "/",
		View:       "full",
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("ListMemories full: %v", err)
	}
	if lastListPrefix != "" {
		t.Fatalf("root path_prefix should be omitted, got %q", lastListPrefix)
	}
	if len(fullList.Items) != 3 {
		t.Fatalf("fullList len = %d, want 3", len(fullList.Items))
	}
	for _, item := range fullList.Items {
		if item.Content == nil || *item.Content == "" {
			t.Fatalf("expected hydrated content for %s", item.ID)
		}
		if !strings.HasPrefix(item.Path, "/") || item.MemoryStoreID != store.ID {
			t.Fatalf("normalized memory mismatch: %+v", item)
		}
	}

	getMemoryHits.Store(0)
	depth := 1
	depthRes, err := rt.ListMemories(ctx, scope, store.ID, agentruntime.ListMemoriesQuery{
		PathPrefix: "/",
		Depth:      &depth,
		View:       "full",
		Order:      "asc",
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("ListMemories depth=1: %v", err)
	}
	if len(depthRes.Items) != 1 || depthRes.Items[0].Path != "/root.md" || depthRes.Items[0].Content == nil || *depthRes.Items[0].Content != "root content" {
		t.Fatalf("unexpected depth items: %+v", depthRes.Items)
	}
	if len(depthRes.Prefixes) != 1 || depthRes.Prefixes[0] != "/notes/" {
		t.Fatalf("unexpected depth prefixes: %+v", depthRes.Prefixes)
	}
	if getMemoryHits.Load() != 1 {
		t.Fatalf("depth=1 view=full should only hydrate 1 direct child memory, got %d hits", getMemoryHits.Load())
	}

	subDepth, err := rt.ListMemories(ctx, scope, store.ID, agentruntime.ListMemoriesQuery{
		PathPrefix: "/notes/",
		Depth:      &depth,
		View:       "basic",
		Order:      "desc",
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("ListMemories subDepth: %v", err)
	}
	if len(subDepth.Items) != 1 || subDepth.Items[0].Path != "/notes/hello.md" {
		t.Fatalf("unexpected subDepth items: %+v", subDepth.Items)
	}
	if len(subDepth.Prefixes) != 1 || subDepth.Prefixes[0] != "/notes/sub/" {
		t.Fatalf("unexpected subDepth prefixes: %+v", subDepth.Prefixes)
	}

	renamedPath := "/notes/renamed.md"
	updatedMem, err := rt.UpdateMemory(ctx, scope, store.ID, "mem_2", agentruntime.UpdateMemoryInput{
		Path: &renamedPath,
		View: "basic",
	})
	if err != nil {
		t.Fatalf("UpdateMemory: %v", err)
	}
	if lastUpdatePath != "notes/renamed.md" || updatedMem.Path != "/notes/renamed.md" {
		t.Fatalf("update path normalization failed: outbound=%q inbound=%q", lastUpdatePath, updatedMem.Path)
	}

	expectedHash := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if err := rt.DeleteMemory(ctx, scope, store.ID, "mem_2", agentruntime.DeleteMemoryInput{ExpectedContentSHA256: &expectedHash}); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if lastDeleteExpectedHash != expectedHash {
		t.Fatalf("delete expected hash = %q, want %q", lastDeleteExpectedHash, expectedHash)
	}

	versions, err := rt.ListMemoryVersions(ctx, scope, store.ID, agentruntime.ListMemoryVersionsQuery{
		Path: "/root.md",
		View: "full",
	})
	if err != nil {
		t.Fatalf("ListMemoryVersions: %v", err)
	}
	if lastVersionPathFilter != "root.md" {
		t.Fatalf("version path filter = %q, want root.md", lastVersionPathFilter)
	}
	if getVersionHits.Load() != 1 {
		t.Fatalf("expected 1 version content hydrate (skipping redacted), got %d", getVersionHits.Load())
	}
	if len(versions.Items) != 2 || versions.Items[0].MemoryStoreID != store.ID || versions.Items[0].Path == nil || *versions.Items[0].Path != "/root.md" {
		t.Fatalf("unexpected versions[0]: %+v", versions.Items[0])
	}
	if versions.Items[0].Content == nil || *versions.Items[0].Content != "root content" {
		t.Fatalf("expected hydrated version content: %+v", versions.Items[0])
	}
	if !versions.Items[1].Redacted || versions.Items[1].Content != nil {
		t.Fatalf("redacted version must have nil content: %+v", versions.Items[1])
	}

	redacted, err := rt.RedactMemoryVersion(ctx, scope, store.ID, "memver_1")
	if err != nil {
		t.Fatalf("RedactMemoryVersion: %v", err)
	}
	if !redacted.Redacted || redacted.RedactedAt == nil || redacted.Content != nil || redacted.MemoryStoreID != store.ID {
		t.Fatalf("unexpected redacted version: %+v", redacted)
	}
}

func TestQoderSessionMemoryStoreResourceTranslation(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	var seenCreateResourceStoreID string
	var seenAddResourceStoreID string
	var upstreamHits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/agents":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "agt_r1", "version": 1, "name": "A", "model": "qmodel", "created_at": now, "updated_at": now})
		case r.Method == http.MethodPost && r.URL.Path == "/environments":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "env_r1", "name": "E", "created_at": now, "updated_at": now})
		case r.Method == http.MethodPost && r.URL.Path == "/memory_stores":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "memstore_remote_s1", "type": "memory_store", "name": "S1", "status": "active", "created_at": now, "updated_at": now})
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			var req struct {
				Resources []map[string]any `json:"resources"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.Resources) == 1 {
				seenCreateResourceStoreID, _ = req.Resources[0]["memory_store_id"].(string)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "ses_remote_1",
				"agent":          map[string]any{"id": "agt_r1", "version": 1},
				"environment_id": "env_r1",
				"status":         "idle",
				"resources": []map[string]any{
					{
						"id":              "res_remote_1",
						"type":            "memory_store",
						"memory_store_id": "memstore_remote_s1",
						"access":          "read_write",
						"created_at":      now,
						"updated_at":      now,
					},
				},
				"created_at": now,
				"updated_at": now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/ses_remote_1/resources":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			seenAddResourceStoreID, _ = req["memory_store_id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              "res_remote_2",
				"type":            "memory_store",
				"memory_store_id": "memstore_remote_s1",
				"access":          "read_only",
				"created_at":      now,
				"updated_at":      now,
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()
	scopeA := agentruntime.Scope{
		OrganizationUUID:   "org-a",
		WorkspaceUUID:      "ws-a",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	scopeB := agentruntime.Scope{
		OrganizationUUID:   "org-b",
		WorkspaceUUID:      "ws-b",
		RuntimeProfileUUID: "prof-shared-qoder",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}

	agent, err := rt.CreateAgent(ctx, scopeA, agentruntime.CreateAgentInput{Name: "A", Model: "qmodel"})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	env, err := rt.CreateEnvironment(ctx, scopeA, agentruntime.CreateEnvironmentInput{Name: "E"})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	store, err := rt.CreateMemoryStore(ctx, scopeA, agentruntime.CreateMemoryStoreInput{Name: "S1"})
	if err != nil {
		t.Fatalf("CreateMemoryStore: %v", err)
	}

	sess, err := rt.CreateSession(ctx, scopeA, agentruntime.CreateSessionInput{
		AgentID:       agent.ID,
		EnvironmentID: env.ID,
		Resources: []agentruntime.AddSessionResourceInput{
			{
				Type:          "memory_store",
				MemoryStoreID: store.ID,
				Payload:       json.RawMessage(`{"type":"memory_store","memory_store_id":"` + store.ID + `","access":"read_write"}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if seenCreateResourceStoreID != "memstore_remote_s1" {
		t.Fatalf("outbound create resource memory_store_id = %q, want memstore_remote_s1", seenCreateResourceStoreID)
	}
	if len(sess.Resources) != 1 || sess.Resources[0].MemoryStoreID != store.ID {
		t.Fatalf("mapped session resources mismatch: %+v", sess.Resources)
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(sess.Resources[0].Payload, &payloadMap); err != nil {
		t.Fatalf("unmarshal resource payload: %v", err)
	}
	if payloadMap["memory_store_id"] != store.ID {
		t.Fatalf("payload memory_store_id = %v, want %q", payloadMap["memory_store_id"], store.ID)
	}

	added, err := rt.AddSessionResource(ctx, scopeA, sess.ID, agentruntime.AddSessionResourceInput{
		Type:    "memory_store",
		Payload: json.RawMessage(`{"type":"memory_store","memory_store_id":"` + store.ID + `","access":"read_only"}`),
	})
	if err != nil {
		t.Fatalf("AddSessionResource: %v", err)
	}
	if seenAddResourceStoreID != "memstore_remote_s1" {
		t.Fatalf("outbound add resource memory_store_id = %q, want memstore_remote_s1", seenAddResourceStoreID)
	}
	if added.MemoryStoreID != store.ID {
		t.Fatalf("added resource memory_store_id = %q, want %q", added.MemoryStoreID, store.ID)
	}

	hitsBeforeIDOR := upstreamHits.Load()
	_, err = rt.CreateSession(ctx, scopeB, agentruntime.CreateSessionInput{
		AgentID:       agent.ID,
		EnvironmentID: env.ID,
		Resources: []agentruntime.AddSessionResourceInput{
			{Type: "memory_store", MemoryStoreID: store.ID},
		},
	})
	if !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant CreateSession err = %v, want ErrResourceNotFound", err)
	}
	if upstreamHits.Load() != hitsBeforeIDOR {
		t.Fatalf("cross-tenant CreateSession hit upstream unexpectedly")
	}
}

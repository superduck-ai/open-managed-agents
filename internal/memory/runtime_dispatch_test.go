package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type stubMemoryProfileStore struct {
	profile db.RuntimeProfile
}

func (s *stubMemoryProfileStore) GetRuntimeProfileByUUID(context.Context, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubMemoryProfileStore) GetWorkspaceRuntimeProfile(context.Context, string, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubMemoryProfileStore) UpdateRuntimeProfileHealth(context.Context, string, string, string, time.Time) error {
	return nil
}

type mockMemoryRuntime struct {
	store             agentruntime.MemoryStore
	memory            agentruntime.Memory
	version           agentruntime.MemoryVersion
	lastUpdateStore   agentruntime.UpdateMemoryStoreInput
	lastListMemories  agentruntime.ListMemoriesQuery
	lastUpdateMemory  agentruntime.UpdateMemoryInput
	lastDeleteMemory  agentruntime.DeleteMemoryInput
	lastListVersions  agentruntime.ListMemoryVersionsQuery
	listMemoriesRes   agentruntime.MemoryListResult
	getStoreErr       error
	createMemoryErr   error
	deleteStoreCalled bool
}

func (m *mockMemoryRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (m *mockMemoryRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (m *mockMemoryRuntime) Close() error { return nil }

func (m *mockMemoryRuntime) CreateMemoryStore(_ context.Context, _ agentruntime.Scope, input agentruntime.CreateMemoryStoreInput) (agentruntime.MemoryStore, error) {
	created := m.store
	created.Name = input.Name
	created.Description = input.Description
	created.Metadata = input.Metadata
	return created, nil
}

func (m *mockMemoryRuntime) GetMemoryStore(context.Context, agentruntime.Scope, string) (agentruntime.MemoryStore, error) {
	if m.getStoreErr != nil {
		return agentruntime.MemoryStore{}, m.getStoreErr
	}
	return m.store, nil
}

func (m *mockMemoryRuntime) ListMemoryStores(context.Context, agentruntime.Scope, agentruntime.ListMemoryStoresQuery) (agentruntime.Page[agentruntime.MemoryStore], error) {
	return agentruntime.Page[agentruntime.MemoryStore]{
		Items:   []agentruntime.MemoryStore{m.store},
		HasMore: true,
	}, nil
}

func (m *mockMemoryRuntime) UpdateMemoryStore(_ context.Context, _ agentruntime.Scope, _ string, input agentruntime.UpdateMemoryStoreInput) (agentruntime.MemoryStore, error) {
	m.lastUpdateStore = input
	updated := m.store
	if input.Name != nil {
		updated.Name = *input.Name
	}
	if input.Description != nil {
		updated.Description = *input.Description
	}
	if input.Metadata != nil {
		updated.Metadata = input.Metadata
	}
	return updated, nil
}

func (m *mockMemoryRuntime) ArchiveMemoryStore(context.Context, agentruntime.Scope, string) (agentruntime.MemoryStore, error) {
	archived := m.store
	ts := time.Date(2026, 11, 27, 2, 0, 0, 0, time.UTC)
	archived.ArchivedAt = &ts
	return archived, nil
}

func (m *mockMemoryRuntime) DeleteMemoryStore(context.Context, agentruntime.Scope, string) error {
	m.deleteStoreCalled = true
	return nil
}

func (m *mockMemoryRuntime) CreateMemory(_ context.Context, _ agentruntime.Scope, storeID string, input agentruntime.CreateMemoryInput) (agentruntime.Memory, error) {
	if m.createMemoryErr != nil {
		return agentruntime.Memory{}, m.createMemoryErr
	}
	mem := m.memory
	mem.MemoryStoreID = storeID
	mem.Path = input.Path
	if input.View == "full" {
		val := input.Content
		mem.Content = &val
	} else {
		mem.Content = nil
	}
	return mem, nil
}

func (m *mockMemoryRuntime) GetMemory(_ context.Context, _ agentruntime.Scope, storeID, memoryID string, query agentruntime.GetMemoryQuery) (agentruntime.Memory, error) {
	mem := m.memory
	mem.ID = memoryID
	mem.MemoryStoreID = storeID
	if query.View == "basic" {
		mem.Content = nil
	}
	return mem, nil
}

func (m *mockMemoryRuntime) ListMemories(_ context.Context, _ agentruntime.Scope, _ string, query agentruntime.ListMemoriesQuery) (agentruntime.MemoryListResult, error) {
	m.lastListMemories = query
	return m.listMemoriesRes, nil
}

func (m *mockMemoryRuntime) UpdateMemory(_ context.Context, _ agentruntime.Scope, storeID, memoryID string, input agentruntime.UpdateMemoryInput) (agentruntime.Memory, error) {
	m.lastUpdateMemory = input
	mem := m.memory
	mem.ID = memoryID
	mem.MemoryStoreID = storeID
	if input.Path != nil {
		mem.Path = *input.Path
	}
	if input.View == "full" && input.Content != nil {
		val := *input.Content
		mem.Content = &val
	}
	return mem, nil
}

func (m *mockMemoryRuntime) DeleteMemory(_ context.Context, _ agentruntime.Scope, _, _ string, input agentruntime.DeleteMemoryInput) error {
	m.lastDeleteMemory = input
	return nil
}

func (m *mockMemoryRuntime) ListMemoryVersions(_ context.Context, _ agentruntime.Scope, _ string, query agentruntime.ListMemoryVersionsQuery) (agentruntime.Page[agentruntime.MemoryVersion], error) {
	m.lastListVersions = query
	return agentruntime.Page[agentruntime.MemoryVersion]{
		Items:      []agentruntime.MemoryVersion{m.version},
		HasMore:    true,
		NextCursor: "ver_cursor_2",
	}, nil
}

func (m *mockMemoryRuntime) GetMemoryVersion(_ context.Context, _ agentruntime.Scope, _ string, _ string, query agentruntime.GetMemoryVersionQuery) (agentruntime.MemoryVersion, error) {
	ver := m.version
	if query.View == "basic" {
		ver.Content = nil
	}
	return ver, nil
}

func (m *mockMemoryRuntime) RedactMemoryVersion(context.Context, agentruntime.Scope, string, string) (agentruntime.MemoryVersion, error) {
	ver := m.version
	ts := time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC)
	ver.Redacted = true
	ver.RedactedAt = &ts
	ver.Content = nil
	return ver, nil
}

func TestMemoryHandlerExternalRuntimeDispatch(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	content := "hello memory"
	path := "/notes/a.md"
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	size := int64(len(content))

	mockRT := &mockMemoryRuntime{
		store: agentruntime.MemoryStore{
			ID:          "memstore_qdr_1",
			Name:        "Team KB",
			Description: "Shared notes",
			Metadata:    map[string]string{"keep": "yes", "drop": "old"},
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		memory: agentruntime.Memory{
			ID:               "mem_1",
			MemoryStoreID:    "memstore_qdr_1",
			MemoryVersionID:  "memver_1",
			Path:             path,
			Content:          &content,
			ContentSizeBytes: size,
			ContentSHA256:    sha,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		version: agentruntime.MemoryVersion{
			ID:               "memver_1",
			MemoryID:         "mem_1",
			MemoryStoreID:    "memstore_qdr_1",
			Operation:        "created",
			Path:             &path,
			Content:          &content,
			ContentSizeBytes: &size,
			ContentSHA256:    &sha,
			CreatedBy:        agentruntime.MemoryActor{Type: "api_actor"},
			CreatedAt:        now,
		},
	}

	registry := agentruntime.NewRegistry()
	if err := registry.Register(agentruntime.ProviderQoderCloud, func(context.Context, agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return mockRT, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	profile := db.RuntimeProfile{
		UUID:         "00000000-0000-4000-8000-000000000090",
		ProviderKind: agentruntime.ProviderQoderCloud.String(),
		Status:       "active",
	}
	resolver := agentruntime.NewResolver(&stubMemoryProfileStore{profile: profile}, registry, nil, nil)
	handler := NewHandler(config.Config{}, nil, nil, nil).WithRuntimeResolver(resolver)

	withPrincipal := func(req *http.Request) *http.Request {
		return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID:   "00000000-0000-4000-8000-000000000010",
			WorkspaceUUID:      "00000000-0000-4000-8000-000000000011",
			RuntimeProvider:    agentruntime.ProviderQoderCloud.String(),
			RuntimeProfileUUID: profile.UUID,
			CredentialType:     auth.CredentialTypeAPIKey,
		}))
	}

	t.Run("store CRUD archive and delete", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Team KB","description":"Shared notes","metadata":{"env":"dev"}}`))))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"memstore_qdr_1"`) {
			t.Fatalf("create store = %d %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/?limit=10", nil)))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"next_page":"1"`) {
			t.Fatalf("list stores = %d %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/memstore_qdr_1", nil)))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"type":"memory_store"`) {
			t.Fatalf("get store = %d %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/memstore_qdr_1", strings.NewReader(`{"name":"Renamed","metadata":{"drop":null,"added":"ok"}}`))))
		if rec.Code != http.StatusOK {
			t.Fatalf("update store = %d %s", rec.Code, rec.Body.String())
		}
		if mockRT.lastUpdateStore.Metadata["keep"] != "yes" || mockRT.lastUpdateStore.Metadata["added"] != "ok" {
			t.Fatalf("expected merged metadata patch, got %+v", mockRT.lastUpdateStore.Metadata)
		}
		if _, exists := mockRT.lastUpdateStore.Metadata["drop"]; exists {
			t.Fatalf("expected null key to be deleted from metadata: %+v", mockRT.lastUpdateStore.Metadata)
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/memstore_qdr_1/archive", nil)))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"archived_at":"2026-11-27T02:00:00Z"`) {
			t.Fatalf("archive store = %d %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodDelete, "/memstore_qdr_1", nil)))
		if rec.Code != http.StatusOK || !mockRT.deleteStoreCalled || !strings.Contains(rec.Body.String(), `"type":"memory_store_deleted"`) {
			t.Fatalf("delete store = %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("memory CRUD list full cap and depth response", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/memstore_qdr_1/memories?view=full", strings.NewReader(`{"path":"/notes/a.md","content":"hello memory"}`))))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"content":"hello memory"`) {
			t.Fatalf("create memory = %d %s", rec.Code, rec.Body.String())
		}

		mockRT.listMemoriesRes = agentruntime.MemoryListResult{
			Items:    []agentruntime.Memory{mockRT.memory},
			NextPage: "cursor_2",
			HasMore:  true,
		}
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/memstore_qdr_1/memories?view=full&limit=50&path_prefix=/notes/", nil)))
		if rec.Code != http.StatusOK || mockRT.lastListMemories.Limit != 20 || !strings.Contains(rec.Body.String(), `"next_page":"cursor_2"`) {
			t.Fatalf("list memories full = %d limit=%d body=%s", rec.Code, mockRT.lastListMemories.Limit, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/memstore_qdr_1/memories?depth=1&order_by=created_at", nil)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("depth with non-path order_by status = %d, want 400", rec.Code)
		}

		rootMem := mockRT.memory
		rootMem.Path = "/root.md"
		mockRT.listMemoriesRes = agentruntime.MemoryListResult{
			Items:    []agentruntime.Memory{rootMem},
			Prefixes: []string{"/notes/"},
			NextPage: "/root.md",
			HasMore:  true,
		}
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/memstore_qdr_1/memories?depth=1&order=asc", nil)))
		if rec.Code != http.StatusOK {
			t.Fatalf("list memories depth = %d %s", rec.Code, rec.Body.String())
		}
		var depthBody struct {
			Data     []map[string]any `json:"data"`
			Prefixes []map[string]any `json:"prefixes"`
			NextPage *string          `json:"next_page"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &depthBody); err != nil {
			t.Fatalf("unmarshal depth response: %v", err)
		}
		if len(depthBody.Data) != 2 || depthBody.Data[0]["type"] != "memory_prefix" || depthBody.Data[1]["path"] != "/root.md" {
			t.Fatalf("unexpected depth data: %+v", depthBody.Data)
		}
		if len(depthBody.Prefixes) != 1 || depthBody.Prefixes[0]["path"] != "/notes/" {
			t.Fatalf("unexpected depth prefixes: %+v", depthBody.Prefixes)
		}
		if depthBody.NextPage == nil || *depthBody.NextPage == "" {
			t.Fatalf("expected encoded depth next_page cursor")
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/memstore_qdr_1/memories/mem_1", nil)))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"content":"hello memory"`) {
			t.Fatalf("get memory = %d %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/memstore_qdr_1/memories/mem_1?view=full", strings.NewReader(`{"content":"updated","precondition":{"type":"content_sha256","content_sha256":"`+sha+`"}}`))))
		if rec.Code != http.StatusOK || mockRT.lastUpdateMemory.Precondition == nil || mockRT.lastUpdateMemory.Precondition.ContentSHA256 != sha {
			t.Fatalf("update memory = %d pre=%+v body=%s", rec.Code, mockRT.lastUpdateMemory.Precondition, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodDelete, "/memstore_qdr_1/memories/mem_1?expected_content_sha256="+sha, nil)))
		if rec.Code != http.StatusOK || mockRT.lastDeleteMemory.ExpectedContentSHA256 == nil || *mockRT.lastDeleteMemory.ExpectedContentSHA256 != sha {
			t.Fatalf("delete memory = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("versions list retrieve redact and upstream errors", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/memstore_qdr_1/memory_versions?memory_id=mem_1&operation=created&view=full&limit=50", nil)))
		if rec.Code != http.StatusOK || mockRT.lastListVersions.Limit != 20 || !strings.Contains(rec.Body.String(), `"next_page":"ver_cursor_2"`) {
			t.Fatalf("list versions = %d limit=%d body=%s", rec.Code, mockRT.lastListVersions.Limit, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/memstore_qdr_1/memory_versions/memver_1", nil)))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"content":"hello memory"`) {
			t.Fatalf("get version = %d %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/memstore_qdr_1/memory_versions/memver_1/redact", nil)))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"redacted_at":"2026-11-27T03:00:00Z"`) {
			t.Fatalf("redact version = %d %s", rec.Code, rec.Body.String())
		}

		mockRT.getStoreErr = apperr.New(apperr.NotFound, "Memory store not found", nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/memstore_missing", nil)))
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found_error"`) {
			t.Fatalf("not found mapping = %d %s", rec.Code, rec.Body.String())
		}
		mockRT.getStoreErr = nil

		mockRT.createMemoryErr = apperr.New(apperr.Conflict, "path already exists", nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/memstore_qdr_1/memories", strings.NewReader(`{"path":"/notes/a.md","content":"dup"}`))))
		if rec.Code != http.StatusConflict {
			t.Fatalf("conflict mapping = %d %s", rec.Code, rec.Body.String())
		}
		mockRT.createMemoryErr = nil
	})
}

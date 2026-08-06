package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"
)

func TestListReportsUnavailableCatalogWithoutFallbackModels(t *testing.T) {
	t.Parallel()
	handler := NewHandler(fakeCatalogReader{err: modelcatalog.ErrUnavailable}, nil)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if response.Body.String() == "" {
		t.Fatal("response body is empty")
	}
}

func TestListAdaptsPublishedCatalogSnapshot(t *testing.T) {
	t.Parallel()
	inputTokens := 32000
	outputTokens := 4096
	var catalogCapabilities modelcatalog.Capabilities
	if err := json.Unmarshal([]byte(`{
		"thinking":{"supported":true},
		"tool_use":{"supported":false},
		"image_input":{"supported":true}
	}`), &catalogCapabilities); err != nil {
		t.Fatalf("decode catalog capabilities: %v", err)
	}
	lastSuccess := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	handler := NewHandler(fakeCatalogReader{snapshot: modelcatalog.Snapshot{
		Models: []modelcatalog.Model{{
			ID:             "provider/model-1",
			DisplayName:    "Provider Model",
			Description:    "catalog-only description",
			MaxInputTokens: &inputTokens,
			MaxTokens:      &outputTokens,
			Capabilities:   catalogCapabilities,
		}},
		LastSuccessAt: &lastSuccess,
	}}, nil)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var payload listResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.FirstID != "provider/model-1" || payload.LastID != "provider/model-1" || len(payload.Data) != 1 {
		t.Fatalf("list response = %#v", payload)
	}
	model := payload.Data[0]
	if model.ID != "provider/model-1" || model.DisplayName != "Provider Model" {
		t.Fatalf("model = %#v", model)
	}
	if strings.Contains(response.Body.String(), "description") {
		t.Fatalf("model contains non-Anthropic description field: %s", response.Body.String())
	}
	known := model.Capabilities.Known()
	if known.Thinking == nil || !*known.Thinking || known.ToolUse == nil || *known.ToolUse || known.ImageInput == nil || !*known.ImageInput {
		t.Fatalf("capabilities = %#v", model.Capabilities)
	}
}

func TestListAppliesAnthropicCursorPagination(t *testing.T) {
	t.Parallel()
	models := make([]modelcatalog.Model, 0, 25)
	for index := range 25 {
		id := fmt.Sprintf("model-%02d", index)
		models = append(models, modelcatalog.Model{ID: id, DisplayName: id})
	}
	handler := NewHandler(fakeCatalogReader{snapshot: modelcatalog.Snapshot{Models: models}}, nil)

	t.Run("default limit", func(t *testing.T) {
		response := serveModelsRequest(handler, "/")
		page := decodeModelsPage(t, response)
		if len(page.Data) != 20 || !page.HasMore || page.FirstID != "model-00" || page.LastID != "model-19" {
			t.Fatalf("page = %#v", page)
		}
	})

	t.Run("after cursor", func(t *testing.T) {
		response := serveModelsRequest(handler, "/?limit=2&after_id=model-01")
		page := decodeModelsPage(t, response)
		if got := modelIDs(page.Data); !slices.Equal(got, []string{"model-02", "model-03"}) || !page.HasMore {
			t.Fatalf("page ids = %v, has_more = %v", got, page.HasMore)
		}
	})

	t.Run("before cursor", func(t *testing.T) {
		response := serveModelsRequest(handler, "/?limit=2&before_id=model-04")
		page := decodeModelsPage(t, response)
		if got := modelIDs(page.Data); !slices.Equal(got, []string{"model-02", "model-03"}) || !page.HasMore {
			t.Fatalf("page ids = %v, has_more = %v", got, page.HasMore)
		}
	})

	for _, query := range []string{"?limit=0", "?limit=1001", "?limit=invalid"} {
		t.Run("invalid "+query, func(t *testing.T) {
			response := serveModelsRequest(handler, "/"+query)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestRetrieveReturnsCatalogModel(t *testing.T) {
	t.Parallel()
	handler := NewHandler(fakeCatalogReader{snapshot: modelcatalog.Snapshot{Models: []modelcatalog.Model{{
		ID:          "model-1",
		DisplayName: "Model One",
	}}}}, nil)

	response := serveModelsRequest(handler, "/model-1")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var model map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &model); err != nil {
		t.Fatalf("decode model: %v", err)
	}
	if model["id"] != "model-1" || model["type"] != "model" || model["display_name"] != "Model One" {
		t.Fatalf("model = %#v", model)
	}

	missing := serveModelsRequest(handler, "/missing")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d: %s", missing.Code, http.StatusNotFound, missing.Body.String())
	}
}

func TestOfficialAnthropicSDKListsAndRetrievesCatalogModels(t *testing.T) {
	t.Parallel()
	inputTokens := 200000
	maxTokens := 32000
	var capabilities modelcatalog.Capabilities
	if err := json.Unmarshal([]byte(`{"thinking":{"supported":true}}`), &capabilities); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(fakeCatalogReader{snapshot: modelcatalog.Snapshot{Models: []modelcatalog.Model{{
		ID:             "model-1",
		DisplayName:    "Model One",
		CreatedAt:      "2026-07-25T00:00:00Z",
		MaxInputTokens: &inputTokens,
		MaxTokens:      &maxTokens,
		Capabilities:   capabilities,
	}}}}, nil)
	server := httptest.NewServer(http.StripPrefix("/v1/models", handler))
	defer server.Close()
	client := anthropic.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(server.URL))

	page, err := client.Models.List(context.Background(), anthropic.ModelListParams{})
	if err != nil {
		t.Fatalf("Models.List(): %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].ID != "model-1" || page.Data[0].MaxInputTokens != 200000 {
		t.Fatalf("Models.List() page = %#v", page)
	}
	retrieved, err := client.Models.Get(context.Background(), "model-1", anthropic.ModelGetParams{})
	if err != nil {
		t.Fatalf("Models.Get(): %v", err)
	}
	if retrieved.ID != "model-1" || retrieved.DisplayName != "Model One" || retrieved.MaxTokens != 32000 {
		t.Fatalf("Models.Get() model = %#v", retrieved)
	}
}

func serveModelsRequest(handler http.Handler, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

func decodeModelsPage(t *testing.T, response *httptest.ResponseRecorder) listResponse {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var page listResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	return page
}

func modelIDs(models []modelResponse) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

type fakeCatalogReader struct {
	snapshot modelcatalog.Snapshot
	err      error
}

func (r fakeCatalogReader) Snapshot(context.Context) (modelcatalog.Snapshot, error) {
	return r.snapshot, r.err
}

func (r fakeCatalogReader) ValidateModel(context.Context, string) error {
	return errors.New("not implemented")
}

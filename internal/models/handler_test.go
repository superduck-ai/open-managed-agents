package models

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"
)

func TestListReportsUnavailableCatalogWithoutFallbackModels(t *testing.T) {
	t.Parallel()
	handler := NewHandler(fakeCatalogReader{err: modelcatalog.ErrUnavailable})

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
			MaxInputTokens: &inputTokens,
			MaxTokens:      &outputTokens,
			Capabilities:   catalogCapabilities,
		}},
		LastSuccessAt: &lastSuccess,
	}})

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
	if model["id"] != "provider/model-1" || model["display_name"] != "Provider Model" {
		t.Fatalf("model = %#v", model)
	}
	capabilities, _ := model["capabilities"].(map[string]any)
	thinkingCapability, _ := capabilities["thinking"].(map[string]any)
	toolUseCapability, _ := capabilities["tool_use"].(map[string]any)
	imageCapability, _ := capabilities["image_input"].(map[string]any)
	if thinkingCapability["supported"] != true || toolUseCapability["supported"] != false || imageCapability["supported"] != true {
		t.Fatalf("capabilities = %#v", capabilities)
	}
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

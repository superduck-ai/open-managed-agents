package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestListDisplaysMappedModelIDsAndPreservesUnmappedModels(t *testing.T) {
	handler := NewHandler(config.AnthropicUpstreamConfig{ModelMappings: map[string]string{
		"claude-sonnet-4-6": "glm-5-turbo",
		"claude-opus-4-8":   "glm-5.2",
	}})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var body listResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]map[string]any, len(body.Data))
	for _, model := range body.Data {
		modelID, _ := model["id"].(string)
		byID[modelID] = model
	}
	if byID["glm-5-turbo"]["display_name"] != "glm-5-turbo" {
		t.Fatalf("mapped Sonnet = %#v", byID["glm-5-turbo"])
	}
	if byID["glm-5.2"]["display_name"] != "glm-5.2" {
		t.Fatalf("mapped Opus = %#v", byID["glm-5.2"])
	}
	if byID["claude-opus-4-7"]["display_name"] != "Claude Opus 4.7" {
		t.Fatalf("unmapped Opus = %#v", byID["claude-opus-4-7"])
	}
}

func TestResolvePlatformModelsDoesNotMutateInputAndTrimsIDs(t *testing.T) {
	input := []map[string]any{
		{"id": " claude-sonnet-4-6 ", "display_name": "Claude Sonnet 4.6"},
		{"id": "claude-opus-4-7", "display_name": "Claude Opus 4.7"},
	}
	got := resolvePlatformModels(input, map[string]string{"claude-sonnet-4-6": "glm-5-turbo"})
	if input[0]["id"] != " claude-sonnet-4-6 " {
		t.Fatalf("input mutated: %#v", input[0])
	}
	if got[0]["id"] != "glm-5-turbo" || got[0]["display_name"] != "glm-5-turbo" {
		t.Fatalf("mapped model = %#v", got[0])
	}
	if got[1]["id"] != "claude-opus-4-7" || got[1]["display_name"] != "Claude Opus 4.7" {
		t.Fatalf("unmapped model = %#v", got[1])
	}
}

func TestListWithoutMappingsPreservesOriginalCatalog(t *testing.T) {
	handler := NewHandler(config.AnthropicUpstreamConfig{})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	var body listResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.FirstID != "claude-fable-5" || body.LastID != "claude-sonnet-4-5-20250929" {
		t.Fatalf("catalog bounds = %q..%q", body.FirstID, body.LastID)
	}
}

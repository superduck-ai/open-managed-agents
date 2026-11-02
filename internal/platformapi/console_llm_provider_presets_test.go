package platformapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestLLMProviderPresetsRejectModelWrites(t *testing.T) {
	router := chi.NewRouter()
	registerConsoleLLMProviderPresets(router, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/llm_provider_presets/deepseek/models", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("removed write route returned %d", response.Code)
	}
}

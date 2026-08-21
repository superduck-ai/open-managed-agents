package tests

import (
	"net/http"
	"testing"
)

func TestModelsAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("models-bucket"))
	defer app.close()

	t.Run("failure missing api key", func(t *testing.T) {
		resp := app.do(t, http.MethodGet, "/v1/models?limit=1000", nil, "", false, "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("success list models", func(t *testing.T) {
		resp := app.do(t, http.MethodGet, "/v1/models?limit=1000", nil, defaultTestKey, false, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)

		data, ok := body["data"].([]any)
		if !ok || len(data) != len(defaultTestModelIDs) {
			t.Fatalf("models page = %#v", body)
		}
		if len(body) != 1 {
			t.Fatalf("models response has extra fields: %#v", body)
		}

		first, _ := data[0].(map[string]any)
		if len(first) != 2 || first["type"] != "model" || first["id"] != "kimi-k2.5" {
			t.Fatalf("first model = %#v", first)
		}
	})

	t.Run("failure workspace has no provider", func(t *testing.T) {
		clearTestLLMProviders(t, app)
		resp := app.do(t, http.MethodGet, "/v1/models", nil, defaultTestKey, false, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body errorResponse
		decodeJSON(t, resp.Body, &body)
		if body.Error.Type != "api_error" || body.Error.Message != "This workspace has no LLM provider configured" {
			t.Fatalf("error = %#v", body.Error)
		}
		if string(body.Error.Code) != `"workspace_llm_provider_not_configured"` {
			t.Fatalf("error code = %s", body.Error.Code)
		}
	})

	t.Run("success provider has no models", func(t *testing.T) {
		seedTestLLMProvider(t, app, "Empty Provider", "https://empty.example.com", "empty-provider-key")
		resp := app.do(t, http.MethodGet, "/v1/models", nil, defaultTestKey, false, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		data, ok := body["data"].([]any)
		if !ok || len(data) != 0 {
			t.Fatalf("models page = %#v, want empty data", body)
		}
	})
}

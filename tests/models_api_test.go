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
		if body["has_more"] != false || body["first_id"] != "kimi-k2.5" || body["last_id"] != "test" {
			t.Fatalf("models pagination envelope = %#v", body)
		}

		first, _ := data[0].(map[string]any)
		if first["type"] != "model" || first["id"] != "kimi-k2.5" || first["display_name"] != "kimi-k2.5" ||
			first["created_at"] != "1970-01-01T00:00:00Z" {
			t.Fatalf("first model = %#v", first)
		}
		for _, field := range []string{"capabilities", "max_input_tokens", "max_tokens"} {
			if value, exists := first[field]; !exists || value != nil {
				t.Fatalf("first model %s = %#v, want explicit null", field, value)
			}
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
		if body["has_more"] != false || body["first_id"] != nil || body["last_id"] != nil {
			t.Fatalf("empty models pagination envelope = %#v", body)
		}
	})
}

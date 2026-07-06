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
		if !ok || len(data) != 8 || body["has_more"] != false || body["first_id"] != "claude-fable-5" || body["last_id"] != "claude-sonnet-4-5-20250929" {
			t.Fatalf("models page = %#v", body)
		}

		first, _ := data[0].(map[string]any)
		if first["type"] != "model" || first["id"] != "claude-fable-5" || first["display_name"] != "Claude Fable 5" || first["max_input_tokens"] != float64(1000000) || first["max_tokens"] != float64(128000) {
			t.Fatalf("first model = %#v", first)
		}

		capabilities, _ := first["capabilities"].(map[string]any)
		thinking, _ := capabilities["thinking"].(map[string]any)
		thinkingTypes, _ := thinking["types"].(map[string]any)
		effort, _ := capabilities["effort"].(map[string]any)
		xhigh, _ := effort["xhigh"].(map[string]any)
		if capabilities["image_input"] == nil || thinkingTypes["adaptive"] == nil || xhigh["supported"] != true {
			t.Fatalf("first model capabilities = %#v", capabilities)
		}
	})
}

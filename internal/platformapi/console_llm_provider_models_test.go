package platformapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderModelsValidation(t *testing.T) {
	for _, models := range []string{`[{"id":"model","context_window":0}]`, `[{"id":"model","max_output_tokens":0}]`, `[{"id":"model","context_window":-1}]`, `[{"id":"model","max_output_tokens":1.5}]`, `[{"id":"model","context_window":9007199254740992}]`, `[{"id":"model","input_modalities":[]}]`, `[{"id":"model","input_modalities":["audio"]}]`, `[{"id":"model","input_modalities":["text","text"]}]`, `[{"id":""}]`, `[{"id":"model"},{"id":"model"}]`} {
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"name":"Provider","base_url":"https://models.example.com","models":`+models+`}`))
		response := httptest.NewRecorder()
		body, ok := decodeLLMProviderRequest(response, request)
		if ok {
			_, _, _, ok = validateLLMProviderInput(response, body)
		}
		if ok || response.Code != http.StatusBadRequest {
			t.Fatalf("accepted %s", models)
		}
	}
	for _, models := range []string{`[]`, `[{"id":"model"}]`, `[{"id":"model","context_window":1000000,"max_output_tokens":128000,"input_modalities":["text","image"]}]`, `[{"id":"model","input_modalities":["image"]}]`} {
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"name":"Provider","base_url":"https://models.example.com","models":`+models+`}`))
		response := httptest.NewRecorder()
		body, ok := decodeLLMProviderRequest(response, request)
		if ok {
			_, _, _, ok = validateLLMProviderInput(response, body)
		}
		if !ok {
			t.Fatalf("rejected %s: %s", models, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"models":[],"model_ids":[]}`))
	if _, ok := decodeLLMProviderRequest(httptest.NewRecorder(), request); ok {
		t.Fatal("accepted two writable sources")
	}
}

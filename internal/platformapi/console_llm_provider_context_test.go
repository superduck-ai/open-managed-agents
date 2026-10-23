package platformapi

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestProviderContextWindowValidation(t *testing.T) {
	for _, windows := range []string{`{"model":0}`, `{"model":-1}`, `{"model":1.5}`, `{"missing":256000}`, `{"model":9007199254740992}`} {
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"model_ids":["model"],"model_context_windows":`+windows+`}`))
		response := httptest.NewRecorder()
		if _, ok := decodeLLMProviderRequest(response, request); ok || response.Code != http.StatusBadRequest {
			t.Fatalf("accepted invalid windows %s", windows)
		}
	}
	request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"model_ids":["model"],"model_context_windows":{"model":1000000}}`))
	body, ok := decodeLLMProviderRequest(httptest.NewRecorder(), request)
	if !ok || body.ModelContextWindows["model"] != 1000000 {
		t.Fatalf("valid windows rejected: %+v", body)
	}
}

func TestProviderContextWindowsPreserveOmittedAndPruneRemovedModels(t *testing.T) {
	previous := map[string]int{"keep": 256000, "remove": 1000000}
	if got := providerModelValues([]string{"keep"}, nil, previous); !reflect.DeepEqual(got, map[string]int{"keep": 256000}) {
		t.Fatalf("preserve: %v", got)
	}
	if got := providerModelValues([]string{"keep"}, map[string]int{}, previous); len(got) != 0 {
		t.Fatalf("clear: %v", got)
	}
	if got := providerModelValues([]string{"keep"}, map[string]int{"keep": 1000000}, previous); got["keep"] != 1000000 {
		t.Fatalf("replace: %v", got)
	}
	if previous["remove"] != 1000000 {
		t.Fatal("mutated previous configuration")
	}
}

func TestProviderModelOptionsValidation(t *testing.T) {
	for _, options := range []string{`{"model":{"max_output_tokens":0}}`, `{"model":{"max_output_tokens":-1}}`, `{"model":{"max_output_tokens":1.5}}`, `{"missing":{}}`, `{"model":{"input_modalities":[]}}`, `{"model":{"input_modalities":["audio"]}}`, `{"model":{"input_modalities":["text","text"]}}`} {
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"model_ids":["model"],"model_options":`+options+`}`))
		response := httptest.NewRecorder()
		if _, ok := decodeLLMProviderRequest(response, request); ok || response.Code != http.StatusBadRequest {
			t.Fatalf("accepted invalid model options %s", options)
		}
	}
	for _, options := range []string{`{"model":{}}`, `{"model":{"max_output_tokens":128000,"input_modalities":["text","image"]}}`, `{"model":{"input_modalities":["image"]}}`} {
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"model_ids":["model"],"model_options":`+options+`}`))
		if _, ok := decodeLLMProviderRequest(httptest.NewRecorder(), request); !ok {
			t.Fatalf("rejected valid model options %s", options)
		}
	}
}

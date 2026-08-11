package agents

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeSearchRequestRejectsNull(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/agents/search", strings.NewReader(`null`))

	_, err := decodeSearchRequest(httptest.NewRecorder(), request)
	if err == nil || err.Error() != "JSON body must be an object" {
		t.Fatalf("decodeSearchRequest() error = %v, want JSON body must be an object", err)
	}
}

func TestDecodeSearchRequestDecodesObject(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/agents/search", strings.NewReader(`{"name":"agent","limit":10,"include_archived":true,"page":"next"}`))

	body, err := decodeSearchRequest(httptest.NewRecorder(), request)
	if err != nil {
		t.Fatalf("decodeSearchRequest() error = %v", err)
	}
	if body.Name != "agent" || body.Limit == nil || *body.Limit != 10 ||
		body.IncludeArchived == nil || !*body.IncludeArchived || body.Page == nil || *body.Page != "next" {
		t.Fatalf("decodeSearchRequest() = %#v", body)
	}
}

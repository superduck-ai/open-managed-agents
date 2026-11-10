package dsh

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDSRUnknownRoutesUseStructuredErrors(t *testing.T) {
	router := chi.NewRouter()
	ConfigureRouter(router)
	router.Post("/known", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	for _, test := range []struct {
		path   string
		status int
		code   string
	}{{"/unknown", 404, "route_not_found"}, {"/known", 405, "invalid_request"}} {
		response := transcriptRequest(router, "GET", test.path, "", "1")
		if response.Code != test.status {
			t.Fatal(response.Code)
		}
		if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("DSR-Error") != test.code {
			t.Fatalf("headers=%v", response.Header())
		}
	}
}

func TestLifecycleHeadersDoNotChangeNativeResponseBodies(t *testing.T) {
	response := httptest.NewRecorder()
	SetLifecycleErrorHeaders(response, ErrUnauthenticated)
	if response.Header().Get("DSR-Error") != "" {
		t.Fatal("ordinary runtime error classified as lease failure")
	}
	if response.Body.Len() != 0 {
		t.Fatal("native error body was replaced")
	}
}

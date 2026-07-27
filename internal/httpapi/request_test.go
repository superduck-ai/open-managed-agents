package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestBaseURL(t *testing.T) {
	t.Run("request address", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "http://internal.example.test:8080/path", nil)
		if got := RequestBaseURL(request); got != "http://internal.example.test:8080" {
			t.Fatalf("RequestBaseURL() = %q", got)
		}
	})

	t.Run("forwarded address", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "http://internal.example.test:8080/path", nil)
		request.Header.Set("X-Forwarded-Proto", "https, http")
		request.Header.Set("X-Forwarded-Host", "api.example.test, proxy.internal")
		if got := RequestBaseURL(request); got != "https://api.example.test" {
			t.Fatalf("RequestBaseURL() = %q", got)
		}
	})
}

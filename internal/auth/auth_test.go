package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractPlatformSessionKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/files", nil)
	req.AddCookie(&http.Cookie{Name: "sessionKey", Value: "  session-secret  "})

	if got := ExtractPlatformSessionKey(req); got != "session-secret" {
		t.Fatalf("sessionKey = %q, want %q", got, "session-secret")
	}
}

func TestExtractPlatformSessionKeyMissing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/files", nil)

	if got := ExtractPlatformSessionKey(req); got != "" {
		t.Fatalf("sessionKey = %q, want empty", got)
	}
}

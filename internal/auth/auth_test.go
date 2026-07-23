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

func TestExtractBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "mixed case and spaces", header: "  bEaReR signed-token  ", want: "signed-token"},
		{name: "standard", header: "Bearer tok", want: "tok"},
		{name: "empty token", header: "Bearer ", want: ""},
		{name: "missing header", header: "", want: ""},
		{name: "basic auth", header: "Basic abc", want: ""},
		{name: "scheme only", header: "Bearer", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/v1/filestore/fs/listDirectory", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			if got := ExtractBearerToken(req); got != tt.want {
				t.Fatalf("ExtractBearerToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractBearerTokenIgnoresAPIKeyHeader(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v1/filestore/fs/listDirectory", nil)
	request.Header.Set("X-Api-Key", "workspace-key")

	if got := ExtractBearerToken(request); got != "" {
		t.Fatalf("bearer token = %q, want empty", got)
	}
}

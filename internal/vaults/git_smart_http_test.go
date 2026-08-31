package vaults

import (
	"net/http"
	"testing"
)

func TestIsGitSmartHTTPRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		url    string
		want   bool
	}{
		{name: "GET info/refs upload-pack", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack", want: true},
		{name: "GET info/refs receive-pack", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/info/refs?service=git-receive-pack", want: true},
		{name: "POST info/refs upload-pack rejected", method: http.MethodPost, url: "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack", want: false},
		{name: "GET info/refs unrelated service", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-archive", want: false},
		{name: "GET info/refs missing service", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/info/refs", want: false},
		{name: "GET info/refs empty service", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/info/refs?service=", want: false},
		{name: "GET info/refs trailing slash with service", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/info/refs/?service=git-upload-pack", want: true},
		{name: "POST git-upload-pack", method: http.MethodPost, url: "https://gitlab.example.com/group/repo.git/git-upload-pack", want: true},
		{name: "POST git-receive-pack", method: http.MethodPost, url: "https://gitlab.example.com/group/repo.git/git-receive-pack", want: true},
		{name: "GET git-upload-pack rejected", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/git-upload-pack", want: false},
		{name: "GET git-receive-pack rejected", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/git-receive-pack", want: false},
		{name: "POST git-upload-pack trailing slash", method: http.MethodPost, url: "https://gitlab.example.com/group/repo.git/git-upload-pack/", want: true},
		{name: "POST git-receive-pack trailing slash", method: http.MethodPost, url: "https://gitlab.example.com/group/repo.git/git-receive-pack/", want: true},
		{name: "LFS batch path", method: http.MethodPost, url: "https://gitlab.example.com/group/repo.git/info/lfs/objects/batch", want: false},
		{name: "Git REST API", method: http.MethodGet, url: "https://gitlab.example.com/api/v4/user", want: false},
		{name: "suffix lookalike upload-pack", method: http.MethodPost, url: "https://gitlab.example.com/group/repo.git/not-git-upload-pack", want: false},
		{name: "info/refs prefix only", method: http.MethodGet, url: "https://gitlab.example.com/group/repo.git/info/refs-extra?service=git-upload-pack", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tc.method, tc.url, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if got := isGitSmartHTTPRequest(req); got != tc.want {
				t.Fatalf("isGitSmartHTTPRequest(%s %q) = %v, want %v", tc.method, tc.url, got, tc.want)
			}
		})
	}

	t.Run("nil request", func(t *testing.T) {
		t.Parallel()
		if isGitSmartHTTPRequest(nil) {
			t.Fatal("nil request must be false")
		}
	})
	t.Run("nil URL", func(t *testing.T) {
		t.Parallel()
		req := &http.Request{Method: http.MethodGet}
		if isGitSmartHTTPRequest(req) {
			t.Fatal("nil URL must be false")
		}
	})
}

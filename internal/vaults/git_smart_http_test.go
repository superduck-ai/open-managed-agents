package vaults

import (
	"net/http"
	"testing"
)

func TestIsGitSmartHTTPRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{name: "info/refs upload-pack", url: "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack", want: true},
		{name: "info/refs receive-pack", url: "https://gitlab.example.com/group/repo.git/info/refs?service=git-receive-pack", want: true},
		{name: "info/refs unrelated service", url: "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-archive", want: false},
		{name: "info/refs missing service", url: "https://gitlab.example.com/group/repo.git/info/refs", want: false},
		{name: "info/refs empty service", url: "https://gitlab.example.com/group/repo.git/info/refs?service=", want: false},
		{name: "info/refs trailing slash with service", url: "https://gitlab.example.com/group/repo.git/info/refs/?service=git-upload-pack", want: true},
		{name: "git-upload-pack path", url: "https://gitlab.example.com/group/repo.git/git-upload-pack", want: true},
		{name: "git-receive-pack path", url: "https://gitlab.example.com/group/repo.git/git-receive-pack", want: true},
		{name: "git-upload-pack trailing slash", url: "https://gitlab.example.com/group/repo.git/git-upload-pack/", want: true},
		{name: "git-receive-pack trailing slash", url: "https://gitlab.example.com/group/repo.git/git-receive-pack/", want: true},
		{name: "LFS batch path", url: "https://gitlab.example.com/group/repo.git/info/lfs/objects/batch", want: false},
		{name: "Git REST API", url: "https://gitlab.example.com/api/v4/user", want: false},
		{name: "suffix lookalike upload-pack", url: "https://gitlab.example.com/group/repo.git/not-git-upload-pack", want: false},
		{name: "info/refs prefix only", url: "https://gitlab.example.com/group/repo.git/info/refs-extra?service=git-upload-pack", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if got := isGitSmartHTTPRequest(req); got != tc.want {
				t.Fatalf("isGitSmartHTTPRequest(%q) = %v, want %v", tc.url, got, tc.want)
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

package vaults

import (
	"encoding/base64"
	"net/http"
	"strings"
)

const gitSmartHTTPBasicUsername = "oauth2"

// isGitSmartHTTPRequest reports whether req is Git Smart HTTP object transfer.
// Git LFS, dumb HTTP (info/refs without a git service), and Git REST APIs are not.
func isGitSmartHTTPRequest(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	path := strings.TrimRight(req.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/git-upload-pack"), strings.HasSuffix(path, "/git-receive-pack"):
		return true
	case strings.HasSuffix(path, "/info/refs"):
		service := req.URL.Query().Get("service")
		return service == "git-upload-pack" || service == "git-receive-pack"
	default:
		return false
	}
}

func setGitSmartHTTPAuthorization(header http.Header, secret string) {
	userinfo := gitSmartHTTPBasicUsername + ":" + secret
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(userinfo)))
}

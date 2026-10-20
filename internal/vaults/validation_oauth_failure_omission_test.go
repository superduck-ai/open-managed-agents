package vaults

import (
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidationOAuthFailureOmitsRetiredAndCurrentCredentialEcho(t *testing.T) {
	old, current := "retired-upstream-token-739/+private", "current-upstream-token-482/+private"
	bodies := []string{`{"error":"invalid_grant","error_description":"` + old + `"}`, old, `{"error":"invalid_grant","error_description":"` + old + ` ` + current + `"}`, base64.StdEncoding.EncodeToString([]byte(old)), hex.EncodeToString([]byte(old))}
	for _, status := range []int{400, 401, 403, 429, 503} {
		for _, body := range bodies {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
			}))
			capture := newValidationCapture(server.Client(), &mcpOAuthCredentialAuth{}, mcpOAuthCredentialSecret{AccessToken: current}, nil)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("grant_type=refresh_token"))
			if err != nil {
				t.Fatal(err)
			}
			reply, err := capture.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			received, err := io.ReadAll(reply.Body)
			_ = reply.Body.Close()
			server.Close()
			if err != nil || string(received) != body {
				t.Fatal("diagnostic capture changed the actual OAuth failure consumed by the classifier")
			}
			safe := capture.snapshot()
			if safe == nil || safe.StatusCode != status || safe.ContentType != "application/json" || !safe.BodyTruncated || safe.Body != "[REDACTED]" {
				t.Fatal("untrusted OAuth failure text was not omitted while retaining status metadata")
			}
		}
	}
}

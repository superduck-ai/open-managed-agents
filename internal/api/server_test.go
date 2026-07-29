package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

func TestNewServerUsesDefaultLoggerForHTTPAccess(t *testing.T) {
	var logs bytes.Buffer
	previousDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousDefault)
	})

	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create code session credentials: %v", err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create filestore credentials: %v", err)
	}
	server := NewServer(ServerDeps{
		CodeSessionCredentials: credentials,
		FilestoreCredentials:   filestoreCredentials,
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	entries := parseSlogJSONLines(t, logs.String())
	if len(entries) != 2 {
		t.Fatalf("access log entries = %d, want 2: %s", len(entries), logs.String())
	}
	if entries[0]["component"] != "http" || entries[0]["msg"] != "http request" {
		t.Fatalf("unexpected request access log: %#v", entries[0])
	}
	if entries[1]["component"] != "http" || entries[1]["msg"] != "http response" {
		t.Fatalf("unexpected response access log: %#v", entries[1])
	}
}

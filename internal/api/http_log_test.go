package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestRequestLoggingMiddlewareLogsRequestAndResponse(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("component", "http")
	handler := requestLoggingMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/files?beta=true", nil)
	req.Host = "127.0.0.1:18080"
	req = req.WithContext(httpapi.WithRequestID(req.Context(), "req_test"))
	req.Header.Set("User-Agent", "anthropic-sdk-go/1.0.0")
	req.Header.Set("Anthropic-Client-Platform", "claude-ai")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	entries := parseSlogJSONLines(t, buf.String())
	if len(entries) != 2 {
		t.Fatalf("log entries = %d, want 2: %s", len(entries), buf.String())
	}

	requestEntry := entries[0]
	if requestEntry["level"] != "INFO" || requestEntry["msg"] != ">>> GET /v1/files?beta=true" {
		t.Fatalf("unexpected request log: %#v", requestEntry)
	}
	if requestEntry["component"] != "http" ||
		requestEntry["event"] != "request" ||
		requestEntry["requestId"] != "req_test" ||
		requestEntry["method"] != "GET" ||
		requestEntry["url"] != "/v1/files?beta=true" ||
		requestEntry["path"] != "/v1/files" ||
		requestEntry["host"] != "127.0.0.1:18080" ||
		requestEntry["clientKind"] != "web" ||
		requestEntry["anthropicClientPlatform"] != "claude-ai" {
		t.Fatalf("request log fields mismatch: %#v", requestEntry)
	}

	responseEntry := entries[1]
	if responseEntry["level"] != "INFO" || responseEntry["msg"] != "<<< GET /v1/files?beta=true 200" {
		t.Fatalf("unexpected response log: %#v", responseEntry)
	}
	if responseEntry["event"] != "response" || responseEntry["status"] != float64(http.StatusOK) {
		t.Fatalf("response log fields mismatch: %#v", responseEntry)
	}
	if _, ok := responseEntry["durationMs"].(float64); !ok {
		t.Fatalf("durationMs should be numeric: %#v", responseEntry)
	}
}

func TestRequestLoggingMiddlewareLogsNon2xxAsError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("component", "http")
	handler := requestLoggingMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/files?beta=true", nil)
	req.Host = "api.example.test"
	req = req.WithContext(httpapi.WithRequestID(req.Context(), "req_error"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	entries := parseSlogJSONLines(t, buf.String())
	if len(entries) != 2 {
		t.Fatalf("log entries = %d, want 2: %s", len(entries), buf.String())
	}
	responseEntry := entries[1]
	if responseEntry["level"] != "ERROR" ||
		responseEntry["msg"] != "<<< POST /v1/files?beta=true 500" ||
		responseEntry["status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("unexpected error response log: %#v", responseEntry)
	}
}

func parseSlogJSONLines(t *testing.T, logs string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestRequestLoggingMiddlewareLogsRequestAndResponse(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).With("component", "http")
	handler := requestLoggingMiddleware(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/files?beta=true&state=secret-state", nil)
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
	if requestEntry["level"] != "DEBUG" || requestEntry["msg"] != "http request" {
		t.Fatalf("unexpected request log: %#v", requestEntry)
	}
	if requestEntry["component"] != "http" ||
		requestEntry["event"] != "request" ||
		requestEntry["request_id"] != "req_test" ||
		requestEntry["method"] != "GET" ||
		requestEntry["url"] != "/v1/files" ||
		requestEntry["path"] != "/v1/files" ||
		requestEntry["host"] != "127.0.0.1:18080" ||
		requestEntry["client_kind"] != "web" ||
		requestEntry["anthropic_client_platform"] != "claude-ai" {
		t.Fatalf("request log fields mismatch: %#v", requestEntry)
	}

	responseEntry := entries[1]
	if responseEntry["level"] != "DEBUG" || responseEntry["msg"] != "http response" {
		t.Fatalf("unexpected response log: %#v", responseEntry)
	}
	if strings.Contains(buf.String(), "secret-state") {
		t.Fatalf("request query leaked into logs: %s", buf.String())
	}
	if responseEntry["event"] != "response" || responseEntry["status"] != float64(http.StatusOK) {
		t.Fatalf("response log fields mismatch: %#v", responseEntry)
	}
	if _, ok := responseEntry["duration_ms"].(float64); !ok {
		t.Fatalf("durationMs should be numeric: %#v", responseEntry)
	}
}

func TestRequestLoggingMiddlewareLogsServerError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("component", "http")
	handler := requestLoggingMiddleware(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/files?beta=true", nil)
	req.Host = "api.example.test"
	req = req.WithContext(httpapi.WithRequestID(req.Context(), "req_error"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	entries := parseSlogJSONLines(t, buf.String())
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1: %s", len(entries), buf.String())
	}
	responseEntry := entries[0]
	if responseEntry["level"] != "ERROR" ||
		responseEntry["msg"] != "http response" ||
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

func TestRequestLoggingResponseLevels(t *testing.T) {
	for _, tc := range []struct {
		status int
		level  string
	}{{500, "ERROR"}, {404, "WARN"}, {400, "WARN"}} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var output bytes.Buffer
			handler := requestLoggingMiddleware(slog.New(slog.NewJSONHandler(&output, nil)), nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/example", nil))
			records := parseSlogJSONLines(t, output.String())
			if len(records) != 1 || records[0]["level"] != tc.level {
				t.Fatalf("records = %v, want %s", records, tc.level)
			}
		})
	}
}

func TestResponseLogLevel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		canceled bool
		elapsed  time.Duration
		longPoll bool
		want     slog.Level
	}{
		{"client canceled server error", clientClosedRequestStatus, true, time.Millisecond, false, slog.LevelInfo},
		{"server error", http.StatusInternalServerError, false, time.Millisecond, false, slog.LevelError},
		{"long-poll server error", http.StatusInternalServerError, false, 25 * time.Second, true, slog.LevelError},
		{"client error", http.StatusConflict, false, time.Millisecond, false, slog.LevelWarn},
		{"slow success", http.StatusOK, false, slowRequestLogThreshold, false, slog.LevelInfo},
		{"idle long poll", http.StatusNoContent, false, 25 * time.Second, true, slog.LevelDebug},
		{"fast redirect", http.StatusFound, false, time.Millisecond, false, slog.LevelDebug},
		{"fast success", http.StatusOK, false, time.Millisecond, false, slog.LevelDebug},
	} {
		if got := responseLogLevel(tc.status, tc.canceled, tc.elapsed, tc.longPoll); got != tc.want {
			t.Fatalf("%s: level = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRequestLoggingClientCancellationAndRouteIDs(t *testing.T) {
	var output bytes.Buffer
	router := chi.NewRouter()
	router.Use(requestLoggingMiddleware(slog.New(slog.NewJSONHandler(&output, nil)), router))
	router.Post("/v1/code/sessions/{code_session_id}/worker/events", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/code/sessions/cse_test/worker/events", nil).WithContext(ctx))
	records := parseSlogJSONLines(t, output.String())
	if len(records) != 1 || records[0]["level"] != "INFO" || records[0]["status"] != float64(clientClosedRequestStatus) || records[0]["code_session_id"] != "cse_test" {
		t.Fatalf("records = %v", records)
	}
}

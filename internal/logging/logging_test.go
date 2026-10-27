package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestConsoleHandlerFormatsHTTPLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewConsoleHandler(&buf, slog.LevelInfo)).With("component", "http")

	logger.Info("http response",
		"event", "response",
		"requestId", "req_test",
		"method", "GET",
		"url", "/v1/files",
		"status", 200,
		"durationMs", 12.3,
		"path", "/v1/files",
		"host", "127.0.0.1:18080",
		"userAgent", "anthropic-sdk-go/1.0.0",
		"clientKind", "api",
	)

	line := strings.TrimSpace(buf.String())
	if strings.Contains(line, "\x1b[") {
		t.Fatalf("non-terminal http log contains ANSI escape codes: %q", line)
	}
	if !strings.Contains(line, " [api] GET 200 12.3ms /v1/files ") {
		t.Fatalf("unexpected http log line: %q", line)
	}
	for _, want := range []string{"requestId=req_test", "path=/v1/files", "host=127.0.0.1:18080"} {
		if !strings.Contains(line, want) {
			t.Fatalf("http log line missing %q: %q", want, line)
		}
	}
}

func TestConsoleHandlerFormatsGenericLineWithoutANSI(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, slog.LevelInfo)
	record := slog.NewRecord(
		time.Date(2026, time.August, 28, 10, 55, 41, 447000000, time.UTC),
		slog.LevelError,
		"request failed",
		0,
	)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if got, want := strings.TrimSpace(buf.String()), "10:55:41.447 ERROR request failed"; got != want {
		t.Fatalf("log line = %q, want %q", got, want)
	}
}

func TestLoggerOrDefault(t *testing.T) {
	t.Run("returns injected logger", func(t *testing.T) {
		injected := slog.New(slog.NewTextHandler(io.Discard, nil))
		if got := LoggerOrDefault(injected); got != injected {
			t.Fatal("LoggerOrDefault() did not preserve the injected logger")
		}
	})

	t.Run("returns process default", func(t *testing.T) {
		if got := LoggerOrDefault(nil); got != slog.Default() {
			t.Fatal("LoggerOrDefault(nil) did not return slog.Default()")
		}
	})
}

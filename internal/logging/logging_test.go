package logging

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
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

	line := stripANSI(strings.TrimSpace(buf.String()))
	if !strings.Contains(line, " [api] GET 200 12.3ms /v1/files ") {
		t.Fatalf("unexpected http log line: %q", line)
	}
	for _, want := range []string{"requestId=req_test", "path=/v1/files", "host=127.0.0.1:18080"} {
		if !strings.Contains(line, want) {
			t.Fatalf("http log line missing %q: %q", want, line)
		}
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

func stripANSI(s string) string {
	replacer := strings.NewReplacer(
		ansiReset, "",
		ansiRed, "",
		ansiGreen, "",
		ansiYellow, "",
		ansiCyan, "",
		ansiGray, "",
	)
	return replacer.Replace(s)
}

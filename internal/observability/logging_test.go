package observability

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestConsoleHandlerFormatsHTTPLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewConsoleHandler(&buf, slog.LevelInfo)).With("component", "http")

	logger.Info("<<< GET /v1/files?beta=true 200",
		"event", "response",
		"requestId", "req_test",
		"method", "GET",
		"url", "/v1/files?beta=true",
		"status", 200,
		"durationMs", 12.3,
		"path", "/v1/files",
		"host", "127.0.0.1:18080",
		"userAgent", "anthropic-sdk-go/1.0.0",
		"clientKind", "api",
	)

	line := stripANSI(strings.TrimSpace(buf.String()))
	if !strings.Contains(line, " [api] GET 200 12.3ms /v1/files?beta=true ") {
		t.Fatalf("unexpected http log line: %q", line)
	}
	for _, want := range []string{"requestId=req_test", "path=/v1/files", "host=127.0.0.1:18080"} {
		if !strings.Contains(line, want) {
			t.Fatalf("http log line missing %q: %q", want, line)
		}
	}
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

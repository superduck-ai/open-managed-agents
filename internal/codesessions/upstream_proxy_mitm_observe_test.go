package codesessions

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type upstreamProxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f upstreamProxyRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type synchronizedLogBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedLogBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *synchronizedLogBuffer) entries(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buffer.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func exchangeThroughUpstreamProxyMITM(t *testing.T, transport http.RoundTripper) (int, []map[string]any, string) {
	t.Helper()
	logOutput := &synchronizedLogBuffer{}
	handler := &Handler{logger: slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	clientSide, serverSide := net.Pipe()
	served := make(chan error, 1)
	go func() {
		served <- handler.serveUpstreamProxyMITMHTTP(context.Background(), serverSide, transport, testUpstreamProxyIdentity(), "git.example.com:443", "git.example.com")
	}()
	request, err := http.NewRequest(http.MethodGet, "https://git.example.com/group/repo.git/info/refs?service=git-upload-pack", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Close = true
	request.Header.Set("Authorization", "Bearer secret-token")
	if err := request.Write(clientSide); err != nil {
		t.Fatalf("write request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clientSide), request)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	_ = clientSide.Close()
	<-served
	logOutput.mu.Lock()
	raw := logOutput.buffer.String()
	logOutput.mu.Unlock()
	return response.StatusCode, logOutput.entries(t), raw
}

func findUpstreamProxyLog(entries []map[string]any, message string) map[string]any {
	for _, entry := range entries {
		if entry["msg"] == message {
			return entry
		}
	}
	return nil
}

func assertUpstreamProxyLogOmitsSecrets(t *testing.T, raw string) {
	t.Helper()
	for _, secret := range []string{"service=git-upload-pack", "secret-token", "response-body"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("upstream proxy log leaked %q: %s", secret, raw)
		}
	}
}

func TestUpstreamProxyMITMLogsTransportFailure(t *testing.T) {
	t.Parallel()
	transport := upstreamProxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	})
	status, entries, raw := exchangeThroughUpstreamProxyMITM(t, transport)
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", status)
	}
	entry := findUpstreamProxyLog(entries, "upstream proxy request failed")
	if entry == nil {
		t.Fatalf("failure log not found: %s", raw)
	}
	if entry["level"] != "WARN" || entry["error_kind"] != "connect" || entry["code_session_id"] != "cse_test" ||
		entry["host"] != "git.example.com" || entry["path"] != "/group/repo.git/info/refs" || entry["method"] != http.MethodGet {
		t.Fatalf("failure log = %#v", entry)
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Fatalf("failure log duration_ms = %#v", entry["duration_ms"])
	}
	if text, _ := entry["error"].(string); !strings.Contains(text, "connection refused") {
		t.Fatalf("failure log error = %#v", entry["error"])
	}
	assertUpstreamProxyLogOmitsSecrets(t, raw)
}

func TestUpstreamProxyMITMLogsRejectedUpstreamStatus(t *testing.T) {
	t.Parallel()
	transport := upstreamProxyRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{"Www-Authenticate": []string{"Basic realm=secret-token"}},
			Body:       io.NopCloser(strings.NewReader("response-body")),
			Request:    request,
		}, nil
	})
	status, entries, raw := exchangeThroughUpstreamProxyMITM(t, transport)
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	entry := findUpstreamProxyLog(entries, "upstream proxy response rejected")
	if entry == nil {
		t.Fatalf("rejected response log not found: %s", raw)
	}
	if entry["level"] != "WARN" || entry["status"] != float64(http.StatusUnauthorized) || entry["code_session_id"] != "cse_test" ||
		entry["host"] != "git.example.com" || entry["path"] != "/group/repo.git/info/refs" || entry["method"] != http.MethodGet {
		t.Fatalf("rejected response log = %#v", entry)
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Fatalf("rejected response log duration_ms = %#v", entry["duration_ms"])
	}
	assertUpstreamProxyLogOmitsSecrets(t, raw)
}

func TestUpstreamProxyMITMDoesNotLogSuccessfulUpstreamStatus(t *testing.T) {
	t.Parallel()
	transport := upstreamProxyRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{"Location": []string{"https://git.example.com/elsewhere"}},
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})
	status, entries, raw := exchangeThroughUpstreamProxyMITM(t, transport)
	if status != http.StatusFound {
		t.Fatalf("status = %d, want 302", status)
	}
	if findUpstreamProxyLog(entries, "upstream proxy response rejected") != nil || findUpstreamProxyLog(entries, "upstream proxy request failed") != nil {
		t.Fatalf("unexpected rejection log: %s", raw)
	}
	if findUpstreamProxyLog(entries, "upstream proxy request received") == nil {
		t.Fatalf("request log not found: %s", raw)
	}
}

func TestUpstreamProxyErrorKind(t *testing.T) {
	t.Parallel()
	cases := map[string]error{
		"credential_injection_rejected": fmt.Errorf("inject: %w", vaults.ErrInjectionRejected),
		"canceled":                      fmt.Errorf("round trip: %w", context.Canceled),
		"timeout":                       fmt.Errorf("round trip: %w", context.DeadlineExceeded),
		"dns":                           &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "git.example.com", IsNotFound: true}},
		"tls":                           fmt.Errorf("handshake: %w", x509.UnknownAuthorityError{}),
		"connect":                       &net.OpError{Op: "dial", Err: errors.New("connection refused")},
		"connection_closed":             io.ErrUnexpectedEOF,
		"other":                         errors.New("unexpected"),
	}
	for want, err := range cases {
		if got := upstreamProxyErrorKind(err); got != want {
			t.Errorf("upstreamProxyErrorKind(%v) = %q, want %q", err, got, want)
		}
	}
}

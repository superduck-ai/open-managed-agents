package messages

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

type proxyErrorReader struct {
	err error
}

func TestHandlerUsesInjectedLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With("component", "messages")
	handler := NewHandler(config.Config{
		AnthropicUpstream: config.AnthropicUpstreamConfig{
			APIKey:  "test-key",
			BaseURL: "%",
		},
	}, logger)
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{
		CredentialType: auth.CredentialTypeAPIKey,
	}))

	handler.Create(httptest.NewRecorder(), request)

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	if entry["component"] != "messages" || entry["msg"] != "build messages upstream endpoint" {
		t.Fatalf("unexpected log entry: %#v", entry)
	}
}

func (r proxyErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestWriteProxyResponseReturnsBodyReadError(t *testing.T) {
	wantErr := errors.New("upstream body failed")
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(proxyErrorReader{err: wantErr}),
	}

	err := writeProxyResponse(httptest.NewRecorder(), response)
	if !errors.Is(err, wantErr) {
		t.Fatalf("write response error = %v, want %v", err, wantErr)
	}
}

func TestWriteProxyResponseCopiesAndFlushes(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := &http.Response{
		StatusCode: http.StatusAccepted,
		Header: http.Header{
			"Content-Type":      []string{"text/event-stream"},
			"Connection":        []string{"X-Connection-Only"},
			"Proxy-Connection":  []string{"keep-alive"},
			"X-Upstream-Test":   []string{"reached"},
			"X-Connection-Only": []string{"must-not-be-forwarded"},
			"Transfer-Encoding": []string{"chunked"},
		},
		Body: io.NopCloser(strings.NewReader("data: hello\n\n")),
	}
	originalHeaders := response.Header.Clone()

	if err := writeProxyResponse(recorder, response); err != nil {
		t.Fatalf("write response: %v", err)
	}
	result := recorder.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusAccepted)
	}
	if result.Header.Get("Content-Type") != "text/event-stream" || result.Header.Get("X-Upstream-Test") != "reached" {
		t.Fatalf("unexpected response headers: %#v", result.Header)
	}
	if result.Header.Get("Transfer-Encoding") != "" ||
		result.Header.Get("Proxy-Connection") != "" ||
		result.Header.Get("X-Connection-Only") != "" {
		t.Fatalf("hop-by-hop response header was forwarded: %#v", result.Header)
	}
	if !reflect.DeepEqual(response.Header, originalHeaders) {
		t.Fatalf("upstream response headers were mutated: got %#v, want %#v", response.Header, originalHeaders)
	}
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if string(body) != "data: hello\n\n" {
		t.Fatalf("body = %q, want SSE event", body)
	}
	if !recorder.Flushed {
		t.Fatal("response was not flushed")
	}
}

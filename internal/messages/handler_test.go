package messages

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type proxyErrorReader struct {
	err error
}

func TestHandlerUsesInjectedLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(nil, nil, logger)
	if handler.logger != logger {
		t.Fatal("NewHandler did not keep the injected logger")
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

func TestReadRequestModelPreservesOriginalBody(t *testing.T) {
	body := `{"model":"kimi-k2.5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	modelID, buffered, err := readRequestModel(strings.NewReader(body))
	if err != nil {
		t.Fatalf("readRequestModel() error = %v", err)
	}
	if modelID != "kimi-k2.5" {
		t.Fatalf("model = %q, want kimi-k2.5", modelID)
	}
	if string(buffered) != body {
		t.Fatalf("buffered body = %q, want original JSON", buffered)
	}
}

func TestReadRequestModelRejectsDuplicateModel(t *testing.T) {
	_, _, err := readRequestModel(strings.NewReader(`{"model":"kimi-k2.5","model":"not-configured"}`))
	if err == nil || err.Error() != "model must appear exactly once" {
		t.Fatalf("readRequestModel() error = %v", err)
	}
}

func TestReadRequestModelPreservesMaxBytesError(t *testing.T) {
	body := `{"model":"kimi-k2.5","messages":[]}`
	limited := http.MaxBytesReader(
		httptest.NewRecorder(),
		io.NopCloser(strings.NewReader(body)),
		int64(len(body)-1),
	)

	_, _, err := readRequestModel(limited)
	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		t.Fatalf("readRequestModel() error = %v, want *http.MaxBytesError", err)
	}
}

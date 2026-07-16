package codesessions

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type runtimeProxyErrorReader struct {
	err error
}

func (r runtimeProxyErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestWriteRuntimeProxyResponseReturnsBodyReadError(t *testing.T) {
	wantErr := errors.New("upstream body failed")
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(runtimeProxyErrorReader{err: wantErr}),
	}

	err := writeRuntimeProxyResponse(httptest.NewRecorder(), response)
	if !errors.Is(err, wantErr) {
		t.Fatalf("write response error = %v, want %v", err, wantErr)
	}
}

func TestWriteRuntimeProxyResponseCopiesAndFlushes(t *testing.T) {
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

	if err := writeRuntimeProxyResponse(recorder, response); err != nil {
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

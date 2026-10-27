package codesessions

import (
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 鉴权失败的 401 行为（含按协议编码的 OTLP 错误体与跨 session token 绕过路径）
// 由 tests/sessions_api_test.go 的 TestCodeSessionWorkerOTLPRejectsInvalidSessionIngress 端到端覆盖。

func TestOTLPEndpointFromPathResolvesSignal(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantSignal string
	}{
		{name: "metrics", path: "/worker/otlp/metrics", wantSignal: "metrics"},
		{name: "standard logs", path: "/worker/otlp/logs", wantSignal: "logs"},
		{name: "detailed logs", path: "/worker/otlp/v1/logs", wantSignal: "logs"},
		{name: "detailed traces", path: "/worker/otlp/v1/traces", wantSignal: "traces"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signal, err := otlpEndpointFromPath(test.path)
			if err != nil || signal != test.wantSignal {
				t.Fatalf("otlpEndpointFromPath() = %q, %v; want %q", signal, err, test.wantSignal)
			}
		})
	}
	if _, err := otlpEndpointFromPath("/worker/otlp/unknown"); err == nil {
		t.Fatal("otlpEndpointFromPath() accepted unsupported endpoint")
	}
}

func TestReadOTLPRequestBodyEnforcesEncodingAndExpandedLimit(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte("expanded payload"))
	_ = writer.Close()
	var compressedLarge bytes.Buffer
	writer = gzip.NewWriter(&compressedLarge)
	_, _ = writer.Write([]byte(strings.Repeat("x", 256)))
	_ = writer.Close()

	tests := []struct {
		name       string
		body       []byte
		encoding   string
		limit      int64
		want       string
		wantStatus int
	}{
		{name: "identity", body: []byte("payload"), limit: 16, want: "payload"},
		{name: "gzip", body: compressed.Bytes(), encoding: "gzip", limit: 128, want: "expanded payload"},
		{name: "expanded too large", body: compressedLarge.Bytes(), encoding: "gzip", limit: int64(len(compressedLarge.Bytes())), wantStatus: http.StatusRequestEntityTooLarge},
		{name: "unsupported", body: []byte("payload"), encoding: "br", limit: 16, wantStatus: http.StatusUnsupportedMediaType},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/otlp", bytes.NewReader(test.body))
			request.Header.Set("Content-Encoding", test.encoding)
			got, err := readOTLPRequestBody(httptest.NewRecorder(), request, test.limit)
			if test.wantStatus == 0 {
				if err != nil || string(got) != test.want {
					t.Fatalf("readOTLPRequestBody() = %q, %v", got, err)
				}
				return
			}
			var bodyError *otlpBodyError
			if err == nil || !strings.Contains(err.Error(), "OTLP") {
				t.Fatalf("readOTLPRequestBody() error = %v", err)
			}
			if !errors.As(err, &bodyError) || bodyError.statusCode != test.wantStatus {
				t.Fatalf("readOTLPRequestBody() status = %#v", bodyError)
			}
		})
	}
}

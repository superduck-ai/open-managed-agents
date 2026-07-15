package codesessions

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"golang.org/x/net/websocket"
)

func TestDecodeUpstreamProxyChunkRejectsInvalidFrames(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		frame []byte
	}{
		{name: "wrong field", frame: []byte{0x12, 0x00}},
		{name: "wrong wire type", frame: []byte{0x08, 0x00}},
		{name: "overlong tag", frame: []byte{0x8a, 0x00, 0x00}},
		{name: "unterminated varint", frame: []byte{0x0a, 0x80}},
		{name: "overlong length varint", frame: []byte{0x0a, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00}},
		{name: "truncated payload", frame: []byte{0x0a, 0x02, 0x01}},
		{name: "trailing bytes", frame: []byte{0x0a, 0x00, 0x01}},
		{name: "trailing field", frame: []byte{0x0a, 0x00, 0x12, 0x00}},
		{name: "repeated data field", frame: []byte{0x0a, 0x00, 0x0a, 0x00}},
		{name: "oversized data", frame: encodeUpstreamProxyChunk(make([]byte, maxUpstreamProxyChunkBytes+1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeUpstreamProxyChunk(test.frame); err == nil {
				t.Fatal("decodeUpstreamProxyChunk() error = nil, want error")
			}
		})
	}
}

func TestUpstreamProxyChunkEncodingAndRoundTrip(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		data        []byte
		wantEncoded []byte
	}{
		{name: "empty", data: []byte{}, wantEncoded: []byte{0x0a, 0x00}},
		{name: "short data", data: []byte("ABC"), wantEncoded: []byte{0x0a, 0x03, 0x41, 0x42, 0x43}},
		{name: "two-byte length", data: bytes.Repeat([]byte{0x5a}, 128)},
		{name: "maximum data", data: bytes.Repeat([]byte{0x5a}, maxUpstreamProxyChunkBytes)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			encoded := encodeUpstreamProxyChunk(test.data)
			if test.wantEncoded != nil && !bytes.Equal(encoded, test.wantEncoded) {
				t.Fatalf("encodeUpstreamProxyChunk() = %x, want %x", encoded, test.wantEncoded)
			}
			decoded, err := decodeUpstreamProxyChunk(encoded)
			if err != nil {
				t.Fatalf("decodeUpstreamProxyChunk() error = %v", err)
			}
			if !bytes.Equal(decoded, test.data) {
				t.Fatal("decodeUpstreamProxyChunk() data does not match input")
			}
		})
	}
}

func TestDecodeUpstreamProxyChunkAcceptsEmptyWebSocketPayload(t *testing.T) {
	t.Parallel()

	decoded, err := decodeUpstreamProxyChunk(nil)
	if err != nil {
		t.Fatalf("decodeUpstreamProxyChunk() error = %v", err)
	}
	if decoded == nil || len(decoded) != 0 {
		t.Fatalf("decodeUpstreamProxyChunk() = %#v, want non-nil empty data", decoded)
	}
}

func TestParseUpstreamProxyConnectRequestRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	validAuthorization := base64.StdEncoding.EncodeToString([]byte("cse_test:cse_test"))
	for _, test := range []struct {
		name string
		head string
	}{
		{name: "not CONNECT", head: "GET example.com:443 HTTP/1.1\r\n\r\n"},
		{name: "missing terminator", head: "CONNECT example.com:443 HTTP/1.1\r\n"},
		{name: "missing authorization", head: "CONNECT example.com:443 HTTP/1.1\r\n\r\n"},
		{name: "invalid authorization", head: "CONNECT example.com:443 HTTP/1.1\r\nProxy-Authorization: Basic invalid\r\n\r\n"},
		{name: "duplicate authorization", head: "CONNECT example.com:443 HTTP/1.1\r\nProxy-Authorization: Basic " + validAuthorization + "\r\nProxy-Authorization: Basic " + validAuthorization + "\r\n\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseUpstreamProxyConnectRequest([]byte(test.head)); err == nil {
				t.Fatal("parseUpstreamProxyConnectRequest() error = nil, want error")
			}
		})
	}
}

func TestResolvePublicUpstreamProxyTargetRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()

	for _, target := range []string{
		"127.0.0.1:443",
		"169.254.169.254:443",
		"10.0.0.1:443",
		"[::1]:443",
		"1.1.1.1:80",
		"missing-port",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			if _, err := resolveUpstreamProxyTarget(context.Background(), target, false); err == nil {
				t.Fatal("resolveUpstreamProxyTarget() error = nil, want error")
			}
		})
	}
}

func TestResolveUpstreamProxyTargetAllowsUnsafeAddressWhenSSRFProtectionDisabled(t *testing.T) {
	t.Parallel()

	// 198.18/15 默认属于被拒绝的 benchmark 网段，也是 Clash/TUN 常见 fake-IP 池；
	// 此用例只证明显式危险开关能够解决本地 fake-IP 冲突。
	target, err := resolveUpstreamProxyTarget(context.Background(), "198.18.10.105:443", true)
	if err != nil {
		t.Fatalf("resolveUpstreamProxyTarget() error = %v", err)
	}
	if target != "198.18.10.105:443" {
		t.Fatalf("target = %q, want %q", target, "198.18.10.105:443")
	}
}

func TestUpstreamProxyCredentialsMustMatchBothFields(t *testing.T) {
	t.Parallel()

	for _, request := range []upstreamProxyConnectRequest{
		{SessionID: "wrong", Token: "cse_test"},
		{SessionID: "cse_test", Token: "wrong"},
	} {
		if upstreamProxyCredentialsMatch(request, "cse_test") {
			t.Fatalf("upstreamProxyCredentialsMatch(%+v) = true, want false", request)
		}
	}
}

func TestGenerateUpstreamProxyCACertificate(t *testing.T) {
	t.Parallel()

	authority, err := generateUpstreamProxyCertificateAuthority()
	if err != nil {
		t.Fatalf("generateUpstreamProxyCertificateAuthority() error = %v", err)
	}
	block, rest := pem.Decode(authority.certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		t.Fatalf("invalid certificate PEM: block=%v rest=%q", block, rest)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	if !certificate.IsCA || certificate.Subject.CommonName == "" {
		t.Fatalf("certificate is not a named CA: %+v", certificate.Subject)
	}
}

func TestUpstreamProxyTunnelForwardsBinaryChunks(t *testing.T) {
	t.Parallel()

	targetConnections := make(chan net.Conn, 1)
	dialTargets := make(chan string, 1)
	handler := NewHandler(config.Config{}, NewService(nil))
	handler.upstreamProxy = upstreamProxyRuntime{
		dial: func(_ context.Context, target string) (net.Conn, error) {
			dialTargets <- target
			proxySide, targetSide := net.Pipe()
			targetConnections <- targetSide
			return proxySide, nil
		},
	}
	server := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(connection *websocket.Conn) {
			handler.serveUpstreamProxyTunnel(connection, "cse_test")
		},
	})
	defer server.Close()

	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http"), "http://localhost")
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	connection, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("DialConfig() error = %v", err)
	}
	defer connection.Close()

	authorization := base64.StdEncoding.EncodeToString([]byte("cse_test:cse_test"))
	connectHead := "CONNECT 1.1.1.1:443 HTTP/1.1\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"
	if err := websocket.Message.Send(connection, encodeUpstreamProxyChunk([]byte(connectHead))); err != nil {
		t.Fatalf("send CONNECT chunk: %v", err)
	}
	status, err := receiveUpstreamProxyChunk(connection)
	if err != nil {
		t.Fatalf("receive CONNECT status: %v", err)
	}
	if !strings.HasPrefix(string(status), "HTTP/1.1 200 Connection Established") {
		t.Fatalf("CONNECT status = %q", status)
	}
	if target := <-dialTargets; target != "1.1.1.1:443" {
		t.Fatalf("dial target = %q, want 1.1.1.1:443", target)
	}

	var target net.Conn
	select {
	case target = <-targetConnections:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for target connection")
	}
	defer target.Close()

	requestPayload := []byte{0x00, 0x01, 0xfe, 0xff}
	if err := websocket.Message.Send(connection, encodeUpstreamProxyChunk(requestPayload)); err != nil {
		t.Fatalf("send request payload: %v", err)
	}
	readPayload := make([]byte, len(requestPayload))
	if _, err := io.ReadFull(target, readPayload); err != nil {
		t.Fatalf("read target payload: %v", err)
	}
	if string(readPayload) != string(requestPayload) {
		t.Fatalf("target payload = %v, want %v", readPayload, requestPayload)
	}

	responsePayload := []byte{0xff, 0x02, 0x00}
	if _, err := target.Write(responsePayload); err != nil {
		t.Fatalf("write target response: %v", err)
	}
	response, err := receiveUpstreamProxyChunk(connection)
	if err != nil {
		t.Fatalf("receive target response: %v", err)
	}
	if string(response) != string(responsePayload) {
		t.Fatalf("response payload = %v, want %v", response, responsePayload)
	}
}

package codesessions

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestHTTPProxyRejectsInvalidRequestsBeforeDial(t *testing.T) {
	for _, row := range []struct {
		name, target, auth string
		status             int
	}{
		{"missing authentication", "http://1.1.1.1:6788/x", "", 407},
		{"wrong session", "http://1.1.1.1:6788/x", "other:token", 407},
		{"HTTPS absolute request", "https://1.1.1.1/x", "cse_test:sk-ant-si-test", 400},
		{"private target", "http://127.0.0.1:6788/x", "cse_test:sk-ant-si-test", 403},
		{"invalid port", "http://1.1.1.1:0/x", "cse_test:sk-ant-si-test", 403},
	} {
		t.Run(row.name, func(t *testing.T) {
			handler := NewHandler(config.Config{}, newTestService(t, nil), nil, nil)
			stubUnrestrictedPolicyContext(t, handler)
			handler.upstreamProxy.dial = func(context.Context, string) (net.Conn, error) {
				t.Error("rejected request dialed")
				return nil, net.ErrClosed
			}
			request := httptest.NewRequest("POST", row.target, strings.NewReader("opaque"))
			if row.auth != "" {
				request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(row.auth)))
			}
			response := httptest.NewRecorder()
			handler.forwardUpstreamProxyHTTP(response, request, testUpstreamProxyIdentity(), "sk-ant-si-test")
			if response.Code != row.status {
				t.Fatalf("status %d, want %d", response.Code, row.status)
			}
		})
	}
}

func TestHTTPProxyForwardsBodyAndStripsProxyAuthentication(t *testing.T) {
	handler := NewHandler(config.Config{}, newTestService(t, nil), nil, nil)
	stubUnrestrictedPolicyContext(t, handler)
	received := make(chan string, 1)
	handler.upstreamProxy.dial = func(_ context.Context, target string) (net.Conn, error) {
		if target != "1.1.1.1:6788" {
			t.Errorf("unexpected target %q", target)
		}
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			request, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				received <- err.Error()
				return
			}
			body, _ := io.ReadAll(request.Body)
			received <- request.URL.RequestURI() + "|" + string(body) + "|" + request.Header.Get("Proxy-Authorization")
			_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		}()
		return client, nil
	}
	request := httptest.NewRequest("POST", "http://1.1.1.1:6788/probe?q=nonce", strings.NewReader("opaque"))
	request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("cse_test:sk-ant-si-test")))
	response := httptest.NewRecorder()
	handler.forwardUpstreamProxyHTTP(response, request, testUpstreamProxyIdentity(), "sk-ant-si-test")
	if response.Code != 200 || response.Body.String() != "ok" {
		t.Fatalf("response %d %q", response.Code, response.Body.String())
	}
	if actual := <-received; actual != "/probe?q=nonce|opaque|" {
		t.Fatalf("upstream request %q", actual)
	}
}

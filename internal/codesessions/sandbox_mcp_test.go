package codesessions

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendSandboxMCPRequestReportsUnsentWhenConnectionIsRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	request := newSandboxMCPTestRequest(t, "http://"+address+"/mcp")

	response, sent, err := sendSandboxMCPRequest(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if err == nil {
		t.Fatal("expected a connection error")
	}
	if sent {
		t.Fatal("refused connection must not be reported as forwarded")
	}
}

func TestSendSandboxMCPRequestReportsSentWhenResponseIsLost(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		connection, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	request := newSandboxMCPTestRequest(t, server.URL+"/mcp")

	response, sent, err := sendSandboxMCPRequest(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if err == nil {
		t.Fatal("expected the lost response to fail")
	}
	if !sent {
		t.Fatal("a request delivered to the sandbox must be reported as forwarded")
	}
	if got := <-received; !bytes.Equal(got, sandboxMCPTestBody) {
		t.Fatalf("sandbox received %q", got)
	}
}

func TestSendSandboxMCPRequestReportsSentOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()
	request := newSandboxMCPTestRequest(t, server.URL+"/mcp")

	response, sent, err := sendSandboxMCPRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if !sent || response.StatusCode != http.StatusOK {
		t.Fatalf("sent=%v status=%d", sent, response.StatusCode)
	}
}

var sandboxMCPTestBody = []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bash","arguments":{"command":"true"}}}`)

func newSandboxMCPTestRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(sandboxMCPTestBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request
}

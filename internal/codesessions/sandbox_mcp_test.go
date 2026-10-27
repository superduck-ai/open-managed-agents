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

func TestSendSandboxMCPRequestReportsUnconnectedWhenConnectionIsRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	request := newSandboxMCPTestRequest(t, "http://"+address+"/mcp")

	response, connected, err := sendSandboxMCPRequest(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if err == nil {
		t.Fatal("expected a connection error")
	}
	if connected {
		t.Fatal("refused connection must not be reported as forwarded")
	}
}

func TestSendSandboxMCPRequestKeepsPartiallyWrittenRequestUnknown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			_, _ = io.ReadFull(connection, make([]byte, 1<<10))
			_ = connection.(*net.TCPConn).SetLinger(0)
			_ = connection.Close()
		}
	}()
	body := bytes.Repeat([]byte("x"), 8<<20)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+listener.Addr().String()+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	response, connected, err := sendSandboxMCPRequest(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if err == nil {
		t.Fatal("expected the reset connection to fail")
	}
	if !connected {
		t.Fatal("a request that reached an open connection must not be reported as unforwarded")
	}
}

func TestSendSandboxMCPRequestReportsConnectedWhenResponseIsLost(t *testing.T) {
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

	response, connected, err := sendSandboxMCPRequest(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if err == nil {
		t.Fatal("expected the lost response to fail")
	}
	if !connected {
		t.Fatal("a request delivered to the sandbox must be reported as forwarded")
	}
	if got := <-received; !bytes.Equal(got, sandboxMCPTestBody) {
		t.Fatalf("sandbox received %q", got)
	}
}

func TestSendSandboxMCPRequestReportsConnectedOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()
	request := newSandboxMCPTestRequest(t, server.URL+"/mcp")

	response, connected, err := sendSandboxMCPRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if !connected || response.StatusCode != http.StatusOK {
		t.Fatalf("connected=%v status=%d", connected, response.StatusCode)
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

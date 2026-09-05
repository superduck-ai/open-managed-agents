package codesessions

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUpstreamProxyEgressRejectsFailedConnect(t *testing.T) {
	for _, response := range []string{
		"HTTP/1.1 407 Proxy Authentication Required\r\n\r\n",
		"HTTP/1.1 502 Bad Gateway\r\n\r\n",
		"invalid response\r\n\r\n",
		"HTTP/1.1 200 OK\r\nX-Large: " + strings.Repeat("x", maxUpstreamProxyConnectHeadBytes) + "\r\n\r\n",
	} {
		t.Run(response[:12], func(t *testing.T) {
			proxyURL, done := startEgressTestProxy(t, func(connection net.Conn, _ *http.Request) {
				_, _ = io.WriteString(connection, response)
			})
			connection, err := dialUpstreamProxyEgress(context.Background(), "1.1.1.1:443", proxyURL)
			if err == nil || connection != nil {
				t.Fatalf("dial = %v, %v; want failure", connection, err)
			}
			<-done
		})
	}
}

func TestUpstreamProxyEgressRejectsUnsupportedScheme(t *testing.T) {
	connection, err := dialUpstreamProxyEgress(context.Background(), "1.1.1.1:443", &url.URL{Scheme: "file", Host: "localhost"})
	if connection != nil || err == nil {
		t.Fatalf("dial = %v, %v; want failure", connection, err)
	}
}

func TestUpstreamProxyEgressRejectsUntrustedHTTPSProxy(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("untrusted proxy must fail TLS before CONNECT is sent")
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	proxyURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := dialUpstreamProxyEgress(context.Background(), "1.1.1.1:443", proxyURL)
	if connection != nil || err == nil {
		t.Fatalf("dial = %v, %v; want certificate failure", connection, err)
	}
}

func TestUpstreamProxyEgressCancellationClosesConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxyURL, done := startEgressTestProxy(t, func(connection net.Conn, _ *http.Request) {
		cancel()
		var buffer [1]byte
		if _, err := connection.Read(buffer[:]); err != io.EOF {
			t.Errorf("read after cancellation = %v, want EOF", err)
		}
	})
	connection, err := dialUpstreamProxyEgress(ctx, "1.1.1.1:443", proxyURL)
	if connection != nil || err == nil {
		t.Fatalf("dial = %v, %v; want cancellation", connection, err)
	}
	<-done
}

func TestUpstreamProxyEgressPinsIPAndPreservesTunnel(t *testing.T) {
	proxyURL, done := startEgressTestProxy(t, func(connection net.Conn, request *http.Request) {
		if request.Method != http.MethodConnect || request.Host != "1.1.1.1:443" || request.RequestURI != "1.1.1.1:443" {
			t.Errorf("unexpected CONNECT: method=%q host=%q target=%q", request.Method, request.Host, request.RequestURI)
		}
		if request.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" || request.Header.Get("Authorization") != "" {
			t.Error("CONNECT must carry only the configured proxy credential")
		}
		_, _ = io.WriteString(connection, "HTTP/1.1 200 Connection Established\r\n\r\n")
		_, _ = io.Copy(connection, connection)
	})
	proxyURL.User = url.UserPassword("user", "pass")
	connection, err := dialUpstreamProxyEgress(context.Background(), "1.1.1.1:443", proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = io.WriteString(connection, "hello")
	buffer := make([]byte, 5)
	if _, err := io.ReadFull(connection, buffer); err != nil || string(buffer) != "hello" {
		t.Fatalf("tunnel echo = %q, %v", buffer, err)
	}
	_ = connection.Close()
	<-done
}

func startEgressTestProxy(t *testing.T, handle func(net.Conn, *http.Request)) (*url.URL, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		request, err := http.ReadRequest(bufio.NewReader(connection))
		if err != nil {
			t.Error(err)
			return
		}
		handle(connection, request)
	}()
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s", listener.Addr()))
	if err != nil {
		t.Fatal(err)
	}
	return proxyURL, done
}
